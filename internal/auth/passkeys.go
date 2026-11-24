package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"docket/internal/database"
	"github.com/go-webauthn/webauthn/webauthn"
)

// PasskeyUser represents the account bound to an opaque WebAuthn user handle.
type PasskeyUser struct {
	User
	handle string
	keys   webauthn.Credentials
}

func (u PasskeyUser) WebAuthnID() []byte                         { return []byte(u.handle) }
func (u PasskeyUser) WebAuthnName() string                       { return u.Username }
func (u PasskeyUser) WebAuthnDisplayName() string                { return u.Username }
func (u PasskeyUser) WebAuthnCredentials() []webauthn.Credential { return u.keys }

// PasskeyInfo is the public metadata for an owner's settings screen.
type PasskeyInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Passkeys lists credentials, including keys enrolled before a hostname change.
func (d *DB) Passkeys(ctx context.Context, owner int64) ([]PasskeyInfo, error) {
	rows, err := d.db.QueryContext(ctx, d.db.SQL("SELECT credential_id,name FROM passkeys WHERE user_id=? ORDER BY name,credential_id"), owner)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // iteration errors are returned by rows.Err
	list := []PasskeyInfo{}
	for rows.Next() {
		var item PasskeyInfo
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// RegistrationUser prepares the stable handle and exclusion list for this relying party.
func (d *DB) RegistrationUser(ctx context.Context, user User, rp string) (PasskeyUser, error) {
	u := PasskeyUser{User: user}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return u, err
	}
	defer rollback(tx)
	if _, err = tx.Exec("INSERT INTO passkey_owners(user_id,handle) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING", user.ID, rand.Text()); err != nil {
		return u, err
	}
	if err = tx.QueryRow("SELECT handle FROM passkey_owners WHERE user_id=?", user.ID).Scan(&u.handle); err != nil {
		return u, err
	}
	rows, err := tx.Query("SELECT credential_id FROM passkeys WHERE user_id=? AND rp=?", user.ID, rp)
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var encoded string
		if err = rows.Scan(&encoded); err != nil {
			_ = rows.Close()
			return u, err
		}
		id, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			_ = rows.Close()
			return u, err
		}
		// Registration only needs IDs for the exclusion list.
		u.keys = append(u.keys, webauthn.Credential{ID: id})
	}
	if err = rows.Err(); err != nil {
		return u, err
	}
	if err = rows.Close(); err != nil {
		return u, err
	}
	return u, tx.Commit()
}

// PasskeyOwner resolves a discoverable assertion through both identifiers and current account status.
func (d *DB) PasskeyOwner(ctx context.Context, rp string, id, handle []byte) (PasskeyUser, error) {
	var u PasskeyUser
	// Our handles are stored as text. An unrelated authenticator may return arbitrary bytes.
	if len(handle) == 0 || len(handle) > 64 || !utf8.Valid(handle) || bytes.IndexByte(handle, 0) >= 0 {
		return u, ErrBadCredentials
	}
	var raw string
	var created int64
	q := d.db.SQL(`SELECT u.id,u.username,u.is_admin,u.created_at,coalesce(b.generation,''),o.handle,k.credential FROM passkeys k
	JOIN passkey_owners o ON o.user_id=k.user_id JOIN users u ON u.id=o.user_id LEFT JOIN workspace_bindings b ON b.user_id=u.id AND b.pending=0
	WHERE k.credential_id=? AND k.rp=? AND o.handle=? AND u.id NOT IN (SELECT user_id FROM account_access WHERE blocked=1)`)
	err := d.db.QueryRowContext(ctx, q, base64.RawURLEncoding.EncodeToString(id), rp, string(handle)).Scan(&u.ID, &u.Username, &u.IsAdmin, &created, &u.WorkspaceKey, &u.handle, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrBadCredentials
	}
	if err != nil {
		return u, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	var key webauthn.Credential
	if err = json.Unmarshal([]byte(raw), &key); err != nil {
		return u, err
	}
	u.keys = webauthn.Credentials{key}
	return u, nil
}

// AddPasskey stores a verified registration under its globally unique credential ID.
func (d *DB) AddPasskey(ctx context.Context, owner int64, rp, name string, key *webauthn.Credential) error {
	raw, err := json.Marshal(key)
	if err != nil {
		return err
	}
	const insert = `INSERT INTO passkeys(credential_id,user_id,rp,name,credential)
	SELECT ?,?,?,?,? WHERE (SELECT count(*) FROM passkeys WHERE user_id=?) < 32`
	res, err := d.db.ExecContext(ctx, d.db.SQL(insert), base64.RawURLEncoding.EncodeToString(key.ID), owner, rp, name, string(raw), owner)
	if database.IsUniqueViolation(err) {
		return ValidationError("passkey already registered")
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ValidationError("passkey limit reached; revoke an unused key")
	}
	return err
}

// UpdatePasskey persists the authenticator counter and backup flags following a verified assertion.
func (d *DB) UpdatePasskey(ctx context.Context, owner int64, key *webauthn.Credential) error {
	raw, err := json.Marshal(key)
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx, d.db.SQL("UPDATE passkeys SET credential=? WHERE user_id=? AND credential_id=?"), string(raw), owner, base64.RawURLEncoding.EncodeToString(key.ID))
	return requirePasskey(result, err)
}

// DeletePasskey revokes one credential belonging to the authenticated owner.
func (d *DB) DeletePasskey(ctx context.Context, owner int64, id string) error {
	res, err := d.db.ExecContext(ctx, d.db.SQL("DELETE FROM passkeys WHERE user_id=? AND credential_id=?"), owner, id)
	return requirePasskey(res, err)
}

func requirePasskey(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
