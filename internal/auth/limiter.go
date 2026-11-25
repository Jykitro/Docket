package auth

import (
	"sync"
	"time"
)

// Limiter refuses a key once it has tried max times within window.
// shortcut: in-memory, resets on restart; move to auth.db if the server runs several replicas.
type Limiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	now       func() time.Time
	fails     map[string][]time.Time
	lastSweep time.Time
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
	if now := l.now(); now.Sub(l.lastSweep) >= time.Minute { // sweep so keys that never return do not accumulate
		l.lastSweep = now
		for k := range l.fails {
			l.recent(k)
		}
	}
	if len(l.recent(key)) >= l.max {
		return false
	}
	l.fails[key] = append(l.fails[key], l.now())
	return true
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
