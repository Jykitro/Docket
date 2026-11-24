package auth

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestPasskeyOwnershipAndRecovery(t *testing.T) {
	d := openTest(t)
	ctx := t.Context()
	alice := newInviter(t, d)
	bob, err := mkUser(d, "bob", "bob-password-123", false)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.RegistrationUser(ctx, alice, "planner.example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.RegistrationUser(ctx, bob, "planner.example.com")
	if err != nil {
		t.Fatal(err)
	}
	key := &webauthn.Credential{ID: []byte{1, 2, 3}}
	if err := d.AddPasskey(ctx, alice.ID, "planner.example.com", "Laptop", key); err != nil {
		t.Fatal(err)
	}
	if err := d.AddPasskey(ctx, bob.ID, "planner.example.com", "Duplicate", key); err == nil {
		t.Fatal("duplicate credential accepted")
	}
	if err := d.DeletePasskey(ctx, bob.ID, base64.RawURLEncoding.EncodeToString(key.ID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign revoke: %v", err)
	}
	for _, tc := range []struct {
		rp     string
		handle []byte
	}{{"planner.example.com", b.WebAuthnID()}, {"other.example.com", a.WebAuthnID()}, {"planner.example.com", []byte{0, 255}}} {
		if _, err := d.PasskeyOwner(ctx, tc.rp, key.ID, tc.handle); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("accepted mismatched binding: %v", err)
		}
	}
	again, err := d.RegistrationUser(ctx, alice, "planner.example.com")
	if err != nil || string(a.WebAuthnID()) != string(again.WebAuthnID()) || len(again.WebAuthnCredentials()) != 1 {
		t.Fatalf("registration handle or exclusions changed: %v", err)
	}
	if err = d.SetBlocked(ctx, alice.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = d.PasskeyOwner(ctx, "planner.example.com", key.ID, a.WebAuthnID()); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("blocked owner: %v", err)
	}
	if err = d.SetBlocked(ctx, alice.ID, false); err != nil {
		t.Fatal(err)
	}
	if owner, err := d.PasskeyOwner(ctx, "planner.example.com", key.ID, a.WebAuthnID()); err != nil || owner.ID != alice.ID {
		t.Fatalf("owner lookup: %v", err)
	}
	code, err := d.IssueRecovery(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Recover(ctx, alice.Username, code, "replacement-123", func(User) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if list, err := d.Passkeys(ctx, alice.ID); err != nil || len(list) != 0 {
		t.Fatalf("keys after recovery: %d, %v", len(list), err)
	}
	if _, err = d.PasskeyOwner(ctx, "planner.example.com", key.ID, a.WebAuthnID()); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("recovered account accepted previous credential: %v", err)
	}
}
