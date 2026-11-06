package auth

import (
	"testing"
	"time"
)

func TestLimiterBlocksAfterMax(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	for i := 0; i < 4; i++ {
		l.Fail("1.2.3.4|a")
	}
	if l.Blocked("1.2.3.4|a") {
		t.Fatal("blocked before reaching max")
	}
	l.Fail("1.2.3.4|a")
	if !l.Blocked("1.2.3.4|a") {
		t.Fatal("not blocked after max failures")
	}
	if l.Blocked("1.2.3.4|b") {
		t.Fatal("other key affected")
	}
}

func TestLimiterWindowExpires(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		l.Fail("k")
	}
	now = now.Add(10*time.Minute + time.Second)
	if l.Blocked("k") {
		t.Fatal("still blocked after window")
	}
	// A later failure sweeps expired keys so the map does not grow without bound.
	l.Fail("other")
	if _, ok := l.fails["k"]; ok {
		t.Fatal("expired key not pruned")
	}
}

func TestLimiterReset(t *testing.T) {
	l := NewLimiter(5, 10*time.Minute)
	for i := 0; i < 5; i++ {
		l.Fail("k")
	}
	l.Reset("k")
	if l.Blocked("k") {
		t.Fatal("blocked after reset")
	}
}
