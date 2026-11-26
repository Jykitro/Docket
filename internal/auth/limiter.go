package auth

import (
	"sync"
	"time"
)

// maxKeys caps the keys one Limiter tracks, so a flood of distinct usernames or addresses cannot grow memory without bound.
const maxKeys = 10_000

type stamp struct {
	key string
	at  time.Time
}

// Limiter refuses a key once it has tried max times within window.
// shortcut: in-memory, resets on restart; move to auth.db if the server runs several replicas.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	fails  map[string][]time.Time
	queue  []stamp // every recorded attempt in time order, drained as it expires: cleanup is amortized O(1) per attempt
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// recent drops expired attempts for key and returns the rest. Caller holds mu.
func (l *Limiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	ts := l.fails[key]
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	if i == len(ts) {
		delete(l.fails, key)
		return nil
	}
	ts = ts[i:]
	l.fails[key] = ts
	return ts
}

// Attempt counts one try for key and reports whether it may proceed; check and record share one lock, so
// concurrent attempts cannot all pass before any is counted.
func (l *Limiter) Attempt(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for len(l.queue) > 0 && !l.queue[0].at.After(now.Add(-l.window)) {
		l.recent(l.queue[0].key)
		l.queue = l.queue[1:]
	}
	ts := l.recent(key)
	if len(ts) >= l.max {
		return false
	}
	// Full: refuse a new key instead of evicting a live one, which would reset that key's protection.
	if _, tracked := l.fails[key]; !tracked && len(l.fails) >= maxKeys {
		return false
	}
	l.fails[key] = append(ts, now)
	l.queue = append(l.queue, stamp{key, now})
	return true
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
