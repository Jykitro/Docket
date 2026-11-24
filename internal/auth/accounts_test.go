package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestRegistrationCapacityAndConcurrency(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if mode, err := d.Registration(ctx); err != nil || mode != "invite" {
		t.Fatalf("default: %q, %v", mode, err)
	}
	admin := newInviter(t, d)
	tok, _, err := d.NewInvite(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := range 2 {
		go func() { _, err := d.Register(ctx, tok, fmt.Sprint("owner", i), "a-safe-password"); results <- err }()
	}
	first, second := <-results, <-results
	if first == nil {
		first, second = second, first
	}
	if second != nil || !errors.Is(first, ErrInvalidInvite) {
		t.Fatalf("double signup: %v, %v", first, second)
	}
	if err = d.SetRegistration(ctx, "open"); err != nil {
		t.Fatal(err)
	}
	for i := 2; i < PilotCapacity-1; i++ {
		if _, err := d.Register(ctx, "", fmt.Sprint("pilot", i), "a-safe-password"); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 2 {
		go func() { _, err := d.Register(ctx, "", fmt.Sprint("finish", i), "a-safe-password"); results <- err }()
	}
	first, second = <-results, <-results
	if first == nil {
		first, second = second, first
	}
	if second != nil || !errors.Is(first, ErrCapacity) {
		t.Fatalf("capacity race: %v, %v", first, second)
	}
}

func TestClosedPolicyWinsAgainstQueuedRegistration(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	newInviter(t, d)
	if err := d.SetRegistration(ctx, "open"); err != nil {
		t.Fatal(err)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec("UPDATE registration SET mode='closed' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := d.Register(ctx, "", "queued", "a-safe-password"); result <- err }()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrRegistration) {
		t.Fatalf("queued registration: %v", err)
	}
}

func TestPasswordProofExpiresBeforeCommit(t *testing.T) {
	d := openTest(t)
	u := newInviter(t, d)
	proof, err := d.VerifyPassword(u.Username, "longenough1")
	if err != nil {
		t.Fatal(err)
	}
	next, err := PreparePassword("new-password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.ReplacePassword(proof, next, ""); err != nil {
		t.Fatal(err)
	}
	if err = d.PasswordCurrent(proof); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("stale proof: %v", err)
	}
	if _, err = d.ReplacePassword(proof, next, ""); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("stale update: %v", err)
	}
}

func TestSessionCapReturnsEvictedSession(t *testing.T) {
	d := openTest(t)
	u := newInviter(t, d)
	for range maxSessionsPerUser {
		if _, err := d.NewSession(u.ID); err != nil {
			t.Fatal(err)
		}
	}
	token, ended, err := d.StartSession(u.ID)
	if err != nil || len(ended) != 1 {
		t.Fatalf("expected one eviction: %d, %v", len(ended), err)
	}
	if _, err = d.UserBySession(token); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = d.db.QueryRow("SELECT count(*) FROM sessions WHERE user_id=?", u.ID).Scan(&n); err != nil || n != maxSessionsPerUser {
		t.Fatalf("sessions=%d %v", n, err)
	}
}
