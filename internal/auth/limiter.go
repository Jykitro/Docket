package auth

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Limiter refuses a key once it has tried max times within window.
// shortcut: in-memory, resets on restart; move to auth.db if the server runs several replicas.
type Limiter struct {
	db        *sql.DB
	namespace string
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
	if l.db != nil {
		return l.attemptShared(key)
	}
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
	if l.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = l.db.ExecContext(ctx, "DELETE FROM vercel_attempts WHERE key=$1", TokenHash(l.namespace+"|"+key))
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// UsePostgres makes a limiter's sliding window shared and persistent across replicas.
func (l *Limiter) UsePostgres(db *sql.DB, namespace string) { l.db = db; l.namespace = namespace }
func (l *Limiter) attemptShared(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	key = TokenHash(l.namespace + "|" + key)
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 39))", key); err != nil {
		return false
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM vercel_attempts WHERE key=$1 AND expires>clock_timestamp()", key).Scan(&count); err != nil || count >= l.max {
		return false
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO vercel_attempts(key,expires) VALUES ($1,clock_timestamp()+$2 * interval '1 second')", key, int64(l.window/time.Second))
	return err == nil && tx.Commit() == nil
}
