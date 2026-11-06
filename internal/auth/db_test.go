package auth

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mkUser(d *DB, username, password string, admin bool) (User, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	u, err := d.createUser(tx, username, password, admin)
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}

func TestUsernameNormalized(t *testing.T) {
	d := openTest(t)
	u, err := mkUser(d, "denis", "longenough1", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "denis" {
		t.Fatalf("username = %q", u.Username)
	}
	if _, err := mkUser(d, " Denis ", "longenough1", false); !errors.Is(err, ErrTaken) {
		t.Fatalf("err = %v, want ErrTaken", err)
	}
	var stored string
	if err := d.db.QueryRow(`SELECT username FROM users`).Scan(&stored); err != nil || stored != "denis" {
		t.Fatalf("stored = %q, %v", stored, err)
	}
	if n, err := d.UserCount(); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestValidation(t *testing.T) {
	d := openTest(t)
	for _, name := range []string{"a", "../x", "has space"} {
		var ve ValidationError
		if _, err := mkUser(d, name, "longenough1", false); !errors.As(err, &ve) {
			t.Errorf("username %q: err = %v, want ValidationError", name, err)
		}
	}
	for _, n := range []int{9, 129} {
		var ve ValidationError
		if _, err := mkUser(d, "okname", strings.Repeat("x", n), false); !errors.As(err, &ve) {
			t.Errorf("password len %d: err = %v, want ValidationError", n, err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	d := openTest(t)
	want, err := mkUser(d, "denis", "longenough1", true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Authenticate("Denis", "longenough1")
	if err != nil || got != want || !got.IsAdmin {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	if _, err := d.Authenticate("denis", "wrong-password"); err != ErrBadCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := d.Authenticate("nobody", "longenough1"); err != ErrBadCredentials {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	d := openTest(t)
	u, err := mkUser(d, "denis", "longenough1", false)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := d.NewSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := d.UserBySession(tok); err != nil || got != u {
		t.Fatalf("got %+v, %v", got, err)
	}
	var n int
	if err := d.db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, tok).Scan(&n); err != nil || n != 0 {
		t.Fatalf("raw token stored: n=%d err=%v", n, err)
	}
	if err := d.db.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash = ?`, hashToken(tok)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("hash not stored: n=%d err=%v", n, err)
	}
	if err := d.DeleteSession(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UserBySession(tok); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}

	tok2, err := d.NewSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	d.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	if _, err := d.UserBySession(tok2); err != ErrNotFound {
		t.Fatalf("expired: %v", err)
	}
}

func TestInvitesSchema(t *testing.T) {
	d := openTest(t)
	rows, err := d.db.Query(`SELECT name FROM pragma_table_info('invites')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	for _, want := range []string{"id", "token_hash", "created_by", "created_at", "expires_at", "used_at"} {
		if !got[want] {
			t.Errorf("invites missing column %q", want)
		}
	}
}
