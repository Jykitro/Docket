package auth

import (
	"sync"
	"time"
)

// Limiter refuses a key once it has failed max times within window.
// shortcut: in-memory, resets on restart; move to auth.db if the server runs several replicas.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	fails  map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, fails: map[string][]time.Time{}}
}

// recent drops expired failures for key and returns the rest. Caller holds mu.
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

func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k := range l.fails { // sweep so keys that never return do not accumulate
		l.recent(k)
	}
	l.fails[key] = append(l.fails[key], l.now())
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
