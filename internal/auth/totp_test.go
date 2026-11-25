package auth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestTOTPEnrollmentReplayAndRecovery(t *testing.T) {
	d := openTest(t)
	if err := d.ConfigureTOTP(bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	u, err := d.SetupAdmin("alice", "password-123")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1900000000, 0)
	d.now = func() time.Time { return now }
	enrollment, err := d.BeginEnrollment(u)
	if err != nil || len(enrollment.QR) == 0 {
		t.Fatal("enrollment", err)
	}
	var ciphertext string
	if err = d.db.QueryRow("SELECT secret_ciphertext FROM user_totp WHERE user_id=?", u.ID).Scan(&ciphertext); err != nil || ciphertext == enrollment.Secret {
		t.Fatal("unprotected secret", err)
	}
	code := func() string {
		v, err := totp.GenerateCode(enrollment.Secret, now)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ctx := t.Context()
	if err = d.SetTOTP(ctx, u.ID, code(), true); err != nil {
		t.Fatal(err)
	}
	challenge, err := d.LoginChallenge(ctx, u.ID)
	if err != nil || challenge == "" {
		t.Fatal("challenge", err)
	}
	if _, err = d.ConsumeChallenge(ctx, challenge, code()); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("enrollment code replayed")
	}
	now = now.Add(30 * time.Second)
	if id, err := d.ConsumeChallenge(ctx, challenge, code()); err != nil || id != u.ID {
		t.Fatal("valid code failed", err)
	}
	if _, err = d.ConsumeChallenge(ctx, challenge, code()); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("challenge replayed")
	}
	challenge, err = d.LoginChallenge(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err = d.ConsumeChallenge(ctx, challenge, code()); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("expired challenge accepted")
	}
	recovery, err := d.IssueRecovery(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Recover(ctx, u.Username, recovery, "changed-password", nil); err != nil {
		t.Fatal(err)
	}
	active, err := d.HasTOTP(u.ID)
	if err != nil || active {
		t.Fatal("recovery retained previous second factor")
	}
}
