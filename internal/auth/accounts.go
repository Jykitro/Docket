package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	"docket/internal/database"
)

// PilotCapacity includes the administrator and blocked profiles, which still occupy storage.
const PilotCapacity = 50

var (
	ErrRegistration = errors.New("registration is unavailable")
	ErrCapacity     = errors.New("workspace limit reached")
	ErrLastAdmin    = errors.New("the last administrator must remain active")
)

// Rolling back a committed transaction returns ErrTxDone. Other failures merit an operator log.
func rollback(tx *database.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Error("auth transaction rollback failed", "err", err)
	}
}

// Registration returns the server-wide signup mode.
func (d *DB) Registration(ctx context.Context) (string, error) {
	var mode string
	err := d.db.QueryRowContext(ctx, "SELECT mode FROM registration WHERE id=1").Scan(&mode)
	return mode, err
}

// SetRegistration serializes mode changes with signup and invite consumption.
func (d *DB) SetRegistration(ctx context.Context, mode string) error {
	switch mode {
	case "open", "invite", "closed":
	default:
		return ValidationError("unknown registration mode")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec("UPDATE registration SET mode=? WHERE id=1", mode); err != nil {
		return err
	}
	return tx.Commit()
}

// Register claims a slot and, if supplied, consumes the invitation in the same transaction.
func (d *DB) Register(ctx context.Context, token, username, password string) (User, error) {
	const policy = "SELECT mode,(SELECT count(*) FROM users) FROM registration WHERE id=1"
	check := func(row *sql.Row) error {
		var mode string
		var count int
		if err := row.Scan(&mode, &count); err != nil {
			return err
		}
		if count == 0 && token != "" {
			return ErrInvalidInvite
		}
		if count == 0 || mode == "closed" || (mode == "invite" && token == "") {
			return ErrRegistration
		}
		if count >= PilotCapacity {
			return ErrCapacity
		}
		return nil
	}
	if err := check(d.db.QueryRowContext(ctx, policy)); err != nil {
		return User{}, err
	}
	if token != "" {
		var valid bool
		err := d.db.QueryRowContext(ctx, d.db.SQL("SELECT EXISTS (SELECT 1 FROM invites WHERE token_hash=? AND used_at IS NULL AND expires_at>?)"), TokenHash(token), d.now().Unix()).Scan(&valid)
		if err != nil {
			return User{}, err
		}
		if !valid {
			return User{}, ErrInvalidInvite
		}
	}
	username, err := normalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	digest, err := PreparePassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer rollback(tx)
	if err = check(tx.QueryRow(policy)); err != nil {
		return User{}, err
	}
	if token != "" {
		if err = d.consumeInvite(tx, token); err != nil {
			return User{}, err
		}
	}
	u, err := d.createUser(tx, username, digest, false)
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}

// Active checks the current access status of an MCP token's owner.
func (d *DB) Active(ctx context.Context, id int64) (bool, error) {
	var active bool
	q := d.db.SQL("SELECT EXISTS (SELECT 1 FROM users WHERE id=? AND is_active=1)")
	err := d.db.QueryRowContext(ctx, q, id).Scan(&active)
	return active, err
}

// ProtectAdministrator prevents disabling the last active administrator.
func (d *DB) ProtectAdministrator(ctx context.Context, id int64) error {
	var last bool
	q := d.db.SQL("SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND is_admin=1) AND NOT EXISTS(SELECT 1 FROM users WHERE id!=? AND is_admin=1 AND is_active=1)")
	if err := d.db.QueryRowContext(ctx, q, id, id).Scan(&last); err != nil {
		return err
	}
	if last {
		return ErrLastAdmin
	}
	return nil
}

// SetBlocked revokes sessions on suspension. Callers serialize access transitions with live streams.
func (d *DB) SetBlocked(ctx context.Context, id int64, blocked bool) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var exists bool
	if err = tx.QueryRow("SELECT EXISTS (SELECT 1 FROM users WHERE id=?)", id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err = tx.Exec("UPDATE users SET is_active=?,updated_at=? WHERE id=?", !blocked, d.now().Unix(), id); err != nil {
		return err
	}
	if blocked {
		if _, err = tx.Exec("DELETE FROM totp_challenges WHERE user_id=?", id); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE workspaces SET mcp_token=NULL,token_digest=NULL WHERE owner_user_id=?", id); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func replaceRecovery(tx *database.Tx, id int64) (string, error) {
	code, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec("INSERT INTO account_access(user_id,recovery_hash) VALUES (?,?) ON CONFLICT(user_id) DO UPDATE SET recovery_hash=excluded.recovery_hash", id, hash)
	return code, err
}

// IssueRecovery replaces the owner's one-use recovery credential.
func (d *DB) IssueRecovery(ctx context.Context, id int64) (string, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	code, err := replaceRecovery(tx, id)
	if err != nil {
		return "", err
	}
	return code, tx.Commit()
}

// Recover consumes a valid credential and rotates all access before releasing the transaction.
func (d *DB) Recover(ctx context.Context, username, code, password string, revokeMCP func(User) error) (string, error) {
	digest, err := d.PrepareRecovery(ctx, username, code, password)
	if err != nil {
		return "", err
	}
	return d.CompleteRecovery(ctx, username, code, digest, revokeMCP)
}

const recoveryOwner = `SELECT u.id,u.username,coalesce(b.generation,'') FROM users u JOIN account_access a ON a.user_id=u.id LEFT JOIN workspace_bindings b ON b.user_id=u.id AND b.pending=0 WHERE u.username=? AND recovery_hash=? AND u.is_active=1`

func readRecoveryOwner(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.WorkspaceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrBadCredentials
	}
	return u, err
}

func (d *DB) PrepareRecovery(ctx context.Context, username, code, password string) (PasswordDigest, error) {
	_, err := readRecoveryOwner(d.db.QueryRowContext(ctx, d.db.SQL(recoveryOwner), strings.ToLower(strings.TrimSpace(username)), TokenHash(code)))
	if err != nil {
		return PasswordDigest{}, err
	}
	return PreparePassword(password)
}

func (d *DB) CompleteRecovery(ctx context.Context, username, code string, digest PasswordDigest, revokeMCP func(User) error) (string, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	u, err := readRecoveryOwner(tx.QueryRow(recoveryOwner, strings.ToLower(strings.TrimSpace(username)), TokenHash(code)))
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec("UPDATE password_credentials SET password_hash=? WHERE user_id=?", digest.hash, u.ID); err != nil {
		return "", err
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", u.ID); err != nil {
		return "", err
	}
	if _, err = tx.Exec("DELETE FROM passkey_owners WHERE user_id=?", u.ID); err != nil {
		return "", err
	}
	next, err := replaceRecovery(tx, u.ID)
	if err != nil {
		return "", err
	}
	for _, q := range []string{"DELETE FROM user_totp WHERE user_id=?", "DELETE FROM totp_challenges WHERE user_id=?", "UPDATE workspaces SET mcp_token=NULL,token_digest=NULL WHERE owner_user_id=?"} {
		if _, err = tx.Exec(q, u.ID); err != nil {
			return "", err
		}
	}
	if revokeMCP != nil {
		if err = revokeMCP(u); err != nil {
			return "", err
		}
	}
	return next, tx.Commit()
}

// WorkspaceSchemas lists only generated tenant schemas in the dedicated PostgreSQL database.
func (d *DB) WorkspaceSchemas(ctx context.Context) ([]int64, error) {
	return d.db.WorkspaceSchemas(ctx)
}

// PendingDeletions records owner-confirmed deletion across crashes between the two stores.
func (d *DB) PendingDeletions(ctx context.Context) ([]User, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT p.user_id,p.username,coalesce(b.generation,''), coalesce(c.confirmation,'') FROM pending_deletions p LEFT JOIN deletion_confirmations c ON c.user_id=p.user_id LEFT JOIN workspace_bindings b ON b.user_id=p.user_id AND b.pending=1")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // read-only cursor; iteration errors are returned below
	var pending []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.WorkspaceKey, &u.DeletionKey); err != nil {
			return nil, err
		}
		pending = append(pending, u)
	}
	return pending, rows.Err()
}

// FinishDeletion acknowledges successful removal of an owner's workspace.
func (d *DB) FinishDeletion(ctx context.Context, id int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec("DELETE FROM pending_deletions WHERE user_id=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM workspace_bindings WHERE user_id=? AND pending=1", id); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveAccount deletes a shared-layout account and its workspaces in one transaction.
func (d *DB) RemoveAccount(id int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec("DELETE FROM workspace_bindings WHERE user_id=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM users WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
