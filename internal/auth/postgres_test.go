package auth

import (
	"errors"
	"sync"
	"testing"

	"plandillo/internal/testutil"
)

// Two independent handles exercise the advisory lock instead of the per-handle connection limit.
func TestPostgresConcurrentSetupAndInvite(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	a, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	run := func(fn func(*DB, int) error, want error) {
		t.Helper()
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i, d := range []*DB{a, b} {
			wg.Add(1)
			go func() { defer wg.Done(); results <- fn(d, i) }()
		}
		wg.Wait()
		close(results)
		successes, refused := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, want) {
				refused++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || refused != 1 {
			t.Fatalf("success=%d refused=%d", successes, refused)
		}
	}
	names := []string{"first", "second"}
	run(func(d *DB, i int) error { _, err := d.SetupAdmin(names[i], "longenough1"); return err }, ErrSetupDone)
	users, err := a.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("users=%v, err=%v", users, err)
	}
	token, _, err := a.NewInvite(users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	run(func(d *DB, i int) error {
		_, err := d.AcceptInvite(token, "invited_"+names[i], "longenough1")
		return err
	}, ErrInvalidInvite)
}
