package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterAttempt(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if !l.Attempt("k") {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if l.Attempt("k") {
		t.Fatal("6th attempt allowed")
	}
	if !l.Attempt("other") {
		t.Fatal("other key affected")
	}
	now = now.Add(10*time.Minute + time.Second)
	if !l.Attempt("k") {
		t.Fatal("still refused after window")
	}
	for l.Attempt("k") {
	}
	l.Reset("k")
	if !l.Attempt("k") {
		t.Fatal("refused after reset")
	}
}

func TestLimiterDropsExpiredKeysOnNextAttempt(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Attempt("gone")
	now = now.Add(9*time.Minute + 30*time.Second)
	l.Attempt("other")
	now = now.Add(31 * time.Second) // "gone" has just expired
	l.Attempt("other")
	if _, ok := l.fails["gone"]; ok {
		t.Fatal("expired key kept after the next attempt")
	}
	if _, ok := l.fails["other"]; !ok {
		t.Fatal("live key dropped")
	}
}

// A flood of distinct keys (usernames, spoofed addresses) must not grow the map without bound,
// must not evict a victim's counter, and must not admit new keys while the map is full.
func TestLimiterBoundsTrackedKeys(t *testing.T) {
	const limit = maxKeys
	l := NewLimiter(5, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Attempt("victim")
	}
	for i := 0; i < limit+1000; i++ {
		l.Attempt(fmt.Sprint("flood", i))
	}
	if n := len(l.fails); n > limit {
		t.Fatalf("tracked %d keys, cap is %d", n, limit)
	}
	if l.Attempt("newcomer") {
		t.Fatal("new key admitted while the map is full")
	}
	if !l.Attempt("victim") {
		t.Fatal("victim's 5th attempt refused")
	}
	if l.Attempt("victim") {
		t.Fatal("victim's counter was reset by the flood")
	}
	now = now.Add(10*time.Minute + time.Second)
	if !l.Attempt("newcomer") {
		t.Fatal("new key refused after the flood expired")
	}
	if n := len(l.fails); n != 1 {
		t.Fatalf("expired keys not evicted: %d left", n)
	}
}
