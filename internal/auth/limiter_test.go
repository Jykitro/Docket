package auth

import (
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

func TestLimiterSweepsExpiredKeys(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Attempt("gone") // first call sweeps (nothing to drop)
	now = now.Add(9*time.Minute + 30*time.Second)
	l.Attempt("other") // sweeps; "gone" is not expired yet
	now = now.Add(31 * time.Second)
	l.Attempt("other") // "gone" has expired, but the last sweep was under a minute ago
	if _, ok := l.fails["gone"]; !ok {
		t.Fatal("swept again within a minute")
	}
	now = now.Add(time.Minute)
	l.Attempt("other")
	if _, ok := l.fails["gone"]; ok {
		t.Fatal("expired key not swept")
	}
}
