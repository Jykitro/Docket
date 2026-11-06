package main

import (
	"slices"
	"testing"
	"time"

	"docket/internal/selfhost"
)

func TestMaintainPass(t *testing.T) {
	last := map[int64]time.Time{}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pass := func(now time.Time, ws ...*selfhost.Workspace) []int64 {
		var got []int64
		maintainPass(ws, last, now, func(w *selfhost.Workspace) { got = append(got, w.ID) })
		return got
	}
	a, b := &selfhost.Workspace{ID: 1}, &selfhost.Workspace{ID: 2}

	if got := pass(t0, a); !slices.Equal(got, []int64{1}) {
		t.Fatalf("first pass: %v", got)
	}
	if got := pass(t0.Add(time.Hour), a, b); !slices.Equal(got, []int64{2}) {
		t.Fatalf("new workspace, recent one skipped: %v", got)
	}
	if got := pass(t0.Add(23*time.Hour), a, b); len(got) != 0 {
		t.Fatalf("within 24h: %v", got)
	}
	if got := pass(t0.Add(24*time.Hour), a, b); !slices.Equal(got, []int64{1}) {
		t.Fatalf("after 24h: %v", got)
	}
	if got := pass(t0.Add(50*time.Hour), a); !slices.Equal(got, []int64{1}) {
		t.Fatalf("removed workspace processed: %v", got)
	}
}
