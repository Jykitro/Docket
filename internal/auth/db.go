package auth

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"plandillo/internal/database"
)

var (
	ErrTaken          = errors.New("username already taken")
	ErrBadCredentials = errors.New("invalid username or password")
	ErrNotFound       = errors.New("not found")
	ErrSetupDone      = errors.New("setup already completed")
	ErrInvalidInvite  = errors.New("invalid or expired invite")
)

// ValidationError is a username/password rule violation, safe to show to the user.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type User struct {
	ID        int64   `json:"id"`
	Username  string  `json:"username"`
	Photo     *string `json:"photo"`
	Name      string  `json:"name"`
	IsActive  bool    `json:"is_active"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	// Compatibility fields consumed by existing administration clients.
	IsAdmin      bool   `json:"is_admin"`
	Blocked      bool   `json:"blocked,omitempty"`
	DeletionKey  string `json:"-"`
	WorkspaceKey string `json:"-"`
}

const (
	sessionTTL = 30 * 24 * time.Hour
	inviteTTL  = 7 * 24 * time.Hour
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{1,31}$`)

// Invite is a pending (unused, unexpired) invite; the token itself is never stored.
type Invite struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS workspace_bindings (user_id INTEGER PRIMARY KEY, generation TEXT NOT NULL UNIQUE, pending INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS sessions (
 id TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL,
 last_seen_at INTEGER NOT NULL,
 user_agent TEXT NOT NULL,
	token_hash TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_owner_expiry ON sessions(user_id,expires_at);
CREATE INDEX IF NOT EXISTS session_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token_hash TEXT NOT NULL UNIQUE,
	created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);
CREATE TABLE IF NOT EXISTS pending_deletions (user_id INTEGER PRIMARY KEY, username TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS deletion_confirmations (user_id INTEGER PRIMARY KEY REFERENCES pending_deletions(user_id) ON DELETE CASCADE, confirmation TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS registration (
 id INTEGER PRIMARY KEY CHECK (id=1),
 mode TEXT NOT NULL CHECK (mode IN ('invite','open','closed'))
);
INSERT INTO registration (id,mode) VALUES (1,'invite') ON CONFLICT (id) DO NOTHING;
CREATE TABLE IF NOT EXISTS account_access (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 recovery_hash TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS passkey_owners (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 handle TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS passkeys (
 credential_id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES passkey_owners(user_id) ON DELETE CASCADE,
 rp TEXT NOT NULL,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT 0,
 credential TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS passkeys_owner ON passkeys(user_id);`

// DB holds users, sessions and invites in a dedicated SQLite file or PostgreSQL schema.
type DB struct {
	factorKey        cipher.AEAD
	db               *database.DB
	now              func() time.Time
	nextSessionSweep atomic.Int64
}

func Open(path string) (*DB, error) {
	db, err := database.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	return initialize(db)
}

func OpenPostgres(dsn string) (*DB, error) {
	db, err := database.OpenPostgres(dsn, database.AuthSchema)
	if err != nil {
		return nil, err
	}
	return initialize(db)
}

func initialize(db *database.DB) (*DB, error) {
	body := schema + factorSchema
	if db.Postgres {
		body = strings.ReplaceAll(body, "INTEGER PRIMARY KEY AUTOINCREMENT", "BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY")
		body = strings.ReplaceAll(body, "INTEGER", "BIGINT")
	}
	tx, err := db.Begin()
	if err == nil {
		defer tx.Rollback()
		err = db.CreateSchema(tx)
		if err == nil {
			err = db.EnsureCatalog(tx)
			if err == nil {
				_, err = tx.Exec(body)
			}
		}
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("auth schema: %w", err)
	}
	return &DB{db: db, now: time.Now}, nil
}

func (d *DB) Connection() *database.DB { return d.db }

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) UserCount() (n int, err error) {
	err = d.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return
}

func validatePassword(password string) error {
	if n := utf8.RuneCountInString(password); n < 10 || n > 128 {
		return ValidationError("password must be 10-128 characters")
	}
	return nil
}

// PasswordDigest is a validated replacement password prepared before acquiring database or access locks.
type PasswordDigest struct{ hash string }

func PreparePassword(password string) (PasswordDigest, error) {
	if err := validatePassword(password); err != nil {
		return PasswordDigest{}, err
	}
	hash, err := HashPassword(password)
	return PasswordDigest{hash: hash}, err
}

func normalizeUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameRe.MatchString(username) {
		return "", ValidationError("username must be 2-32 characters: a-z, 0-9, _ . -, starting with a letter or digit")
	}
	return username, nil
}

func (d *DB) createUser(tx *database.Tx, username string, digest PasswordDigest, admin bool) (User, error) {
	created := d.now().Unix()
	var id int64
	role := "user"
	if admin {
		role = "admin"
	}
	err := tx.QueryRow(`INSERT INTO users (username,name,role,created_at,updated_at) VALUES (?,?,?,?,?) RETURNING id`, username, username, role, created, created).Scan(&id)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return User{}, ErrTaken
		}
		return User{}, err
	}
	if _, err = tx.Exec("INSERT INTO password_credentials(user_id,password_hash) VALUES (?,?)", id, digest.hash); err != nil {
		return User{}, err
	}
	generation := rand.Text()
	slug, err := tx.SpaceSlug(id, username)
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec("INSERT INTO workspaces(owner_user_id,name,slug,generation,created_at,updated_at) VALUES (?,?,?,?,?,?)", id, username, slug, generation, created, created); err != nil {
		return User{}, err
	}
	if _, err = tx.Exec("INSERT INTO workspace_bindings (user_id,generation) VALUES (?,?)", id, generation); err != nil {
		return User{}, err
	}
	return User{Name: username, Role: role, IsActive: true, UpdatedAt: time.Unix(created, 0).UTC().Format(time.RFC3339), ID: id, WorkspaceKey: generation, Username: username, IsAdmin: admin, CreatedAt: time.Unix(created, 0).UTC().Format(time.RFC3339)}, nil
}

// dummyHash lets Authenticate spend the same time on unknown users as on wrong passwords.
var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword("dummy-password")
	return h
})

// PasswordProof is checked again after a concurrent change, recovery or suspension could have invalidated it.
type PasswordProof struct {
	User User
	hash string
}

func (d *DB) PasswordCurrent(p PasswordProof) error {
	var current bool
	err := d.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM users JOIN password_credentials c ON c.user_id=users.id WHERE id=? AND c.password_hash=? AND is_active=1)`, p.User.ID, p.hash).Scan(&current)
	if err == nil && !current {
		return ErrBadCredentials
	}
	return err
}

func (d *DB) Authenticate(username, password string) (User, error) {
	proof, err := d.VerifyPassword(username, password)
	return proof.User, err
}

func (d *DB) VerifyPassword(username, password string) (PasswordProof, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var u User
	var hash string
	var updated, created int64
	err := d.db.QueryRow(`SELECT id, username, password_hash, name,role,is_active,updated_at,CASE WHEN photo IS NOT NULL THEN '/api/users/' || CAST(id AS TEXT) || '/photo' END,is_admin, created_at, coalesce(b.generation,'') FROM users JOIN password_credentials c ON c.user_id=users.id LEFT JOIN workspace_bindings b ON b.user_id=users.id AND b.pending=0 WHERE username = ? AND is_active=1`, username).
		Scan(&u.ID, &u.Username, &hash, &u.Name, &u.Role, &u.IsActive, &updated, &u.Photo, &u.IsAdmin, &created, &u.WorkspaceKey)
	if errors.Is(err, sql.ErrNoRows) {
		CheckPassword(dummyHash(), password)
		return PasswordProof{}, ErrBadCredentials
	}
	if err != nil {
		return PasswordProof{}, err
	}
	if !CheckPassword(hash, password) {
		return PasswordProof{}, ErrBadCredentials
	}
	u.UpdatedAt = time.Unix(updated, 0).UTC().Format(time.RFC3339)
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	return PasswordProof{User: u, hash: hash}, nil
}

// NewSession returns a fresh session token; only its SHA-256 is stored.
func (d *DB) NewSession(userID int64) (string, error) {
	token, _, err := d.StartSession(userID)
	return token, err
}

const maxSessionsPerUser = 32

// StartSession also returns evicted session hashes so the server can end their open streams.
func (d *DB) StartSession(userID int64) (string, []string, error) {
	return d.StartDeviceSession(userID, "")
}
func (d *DB) StartDeviceSession(userID int64, agent string) (string, []string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", nil, err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer rollback(tx)
	now := d.now()
	sweep := now.Unix() >= d.nextSessionSweep.Load()
	if sweep {
		if _, err = tx.Exec("DELETE FROM sessions WHERE expires_at<=?", now.Unix()); err != nil {
			return "", nil, err
		}
	}
	rows, err := tx.Query(`DELETE FROM sessions WHERE user_id=? AND token_hash NOT IN
		(SELECT token_hash FROM sessions WHERE user_id=? ORDER BY expires_at DESC,token_hash DESC LIMIT ?)
		RETURNING token_hash`, userID, userID, maxSessionsPerUser-1)
	if err != nil {
		return "", nil, err
	}
	ended, err := sessionHashes(rows)
	if err != nil {
		return "", nil, err
	}
	if _, err = tx.Exec("INSERT INTO sessions(id,token_hash,user_id,created_at,last_seen_at,expires_at,user_agent) VALUES (?,?,?,?,?,?,?)", rand.Text(), hash, userID, now.Unix(), now.Unix(), now.Add(sessionTTL).Unix(), string([]rune(agent)[:min(256, len([]rune(agent)))])); err != nil {
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	if sweep {
		d.nextSessionSweep.Store(now.Add(time.Hour).Unix())
	}
	return token, ended, nil
}

func (d *DB) UserBySession(token string) (User, error) {
	u, _, err := d.Session(token)
	return u, err
}

// Session returns the session's user and when the session expires.
func (d *DB) Session(token string) (User, time.Time, error) {
	var u User
	var updated, created, expires, seen int64
	// Every signed-in request starts here, so the lookup uses the read pool; the rare last_seen_at update below writes.
	err := d.db.ReadQueryRowContext(context.Background(), `SELECT u.id, u.username, u.name,u.role,u.is_active,u.updated_at,CASE WHEN u.photo IS NOT NULL THEN '/api/users/' || CAST(u.id AS TEXT) || '/photo' END,u.is_admin, u.created_at, s.expires_at,s.last_seen_at, coalesce(b.generation,'') FROM sessions s JOIN users u ON u.id = s.user_id LEFT JOIN workspace_bindings b ON b.user_id=u.id AND b.pending=0
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.is_active=1`, TokenHash(token), d.now().Unix()).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.IsActive, &updated, &u.Photo, &u.IsAdmin, &created, &expires, &seen, &u.WorkspaceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, time.Time{}, ErrNotFound
	}
	if err != nil {
		return User{}, time.Time{}, err
	}
	u.UpdatedAt = time.Unix(updated, 0).UTC().Format(time.RFC3339)
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	if now := d.now().Unix(); now-seen >= 300 {
		if _, err = d.db.Exec("UPDATE sessions SET last_seen_at=? WHERE token_hash=? AND last_seen_at=?", now, TokenHash(token), seen); err != nil {
			return User{}, time.Time{}, err
		}
	}
	return u, time.Unix(expires, 0), nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, TokenHash(token))
	return err
}

// SetupAdmin creates the first user as admin; the emptiness check and insert share one transaction.
func (d *DB) SetupAdmin(username, password string) (User, error) {
	count, err := d.UserCount()
	if err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, ErrSetupDone
	}
	username, err = normalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	digest, err := PreparePassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, ErrSetupDone
	}
	u, err := d.createUser(tx, username, digest, true)
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}

// NewInvite returns a single-use token valid for 7 days; only its SHA-256 is stored.
func (d *DB) NewInvite(createdBy int64) (token string, expires time.Time, err error) {
	token, hash, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := d.now()
	expires = now.Add(inviteTTL)
	res, err := d.db.Exec(`INSERT INTO invites (token_hash, created_by, created_at, expires_at) SELECT ?,?,?,? FROM registration WHERE id=1 AND mode!='closed'`, hash, createdBy, now.Unix(), expires.Unix())
	if err == nil {
		var n int64
		n, err = res.RowsAffected()
		if err == nil && n == 0 {
			err = ErrRegistration
		}
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// AcceptInvite consumes the invite and creates a regular user in one transaction.
func (d *DB) AcceptInvite(token, username, password string) (User, error) {
	if token == "" {
		return User{}, ErrInvalidInvite
	}
	return d.Register(context.Background(), token, username, password)
}

func (d *DB) consumeInvite(tx *database.Tx, token string) error {
	var id int64
	err := tx.QueryRow(`UPDATE invites SET used_at=? WHERE token_hash=? AND used_at IS NULL AND expires_at>? RETURNING id`,
		d.now().Unix(), TokenHash(token), d.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidInvite
	}
	return err
}

// DeleteInvite revokes an unused invite; ErrNotFound when there is none with that id.
func (d *DB) DeleteInvite(id int64) error {
	res, err := d.db.Exec(`DELETE FROM invites WHERE id = ? AND used_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListInvites() ([]Invite, error) {
	rows, err := d.db.Query(`SELECT id, created_at, expires_at FROM invites WHERE used_at IS NULL AND expires_at > ? ORDER BY id`, d.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		var created, expires int64
		if err := rows.Scan(&inv.ID, &created, &expires); err != nil {
			return nil, err
		}
		inv.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
		inv.ExpiresAt = time.Unix(expires, 0).UTC().Format(time.RFC3339)
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.db.Query(`SELECT u.id, username, u.name,u.role,u.is_active,u.updated_at,CASE WHEN u.photo IS NOT NULL THEN '/api/users/' || CAST(u.id AS TEXT) || '/photo' END,is_admin, created_at, (1-u.is_active), coalesce(b.generation,'') FROM users u LEFT JOIN account_access a ON a.user_id=u.id LEFT JOIN workspace_bindings b ON b.user_id=u.id AND b.pending=0 ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var updated, created int64
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.IsActive, &updated, &u.Photo, &u.IsAdmin, &created, &u.Blocked, &u.WorkspaceKey); err != nil {
			return nil, err
		}
		u.UpdatedAt = time.Unix(updated, 0).UTC().Format(time.RFC3339)
		u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser removes the user and their sessions.
func (d *DB) DeleteUser(id int64, confirmation string) error {
	if confirmation == "" {
		return ValidationError("missing workspace deletion confirmation")
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO pending_deletions(user_id,username) SELECT id,username FROM users WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE workspace_bindings SET pending=1 WHERE user_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if _, err = tx.Exec("INSERT INTO deletion_confirmations(user_id,confirmation) VALUES (?,?)", id, confirmation); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangePassword sets a new password and ends every other session of the account, returning their token hashes.
func (d *DB) ChangePassword(userID int64, oldPw, newPw, keepToken string) (ended []string, err error) {
	var username string
	err = d.db.QueryRow("SELECT username FROM users WHERE id=?", userID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	proof, err := d.VerifyPassword(username, oldPw)
	if err != nil {
		return nil, err
	}
	digest, err := PreparePassword(newPw)
	if err != nil {
		return nil, err
	}
	return d.ReplacePassword(proof, digest, keepToken)
}

func (d *DB) ReplacePassword(proof PasswordProof, digest PasswordDigest, keepToken string) (ended []string, err error) {
	userID := proof.User.ID
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE password_credentials SET password_hash = ? WHERE user_id = ? AND password_hash=?`, digest.hash, userID, proof.hash)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, ErrBadCredentials
	}
	if _, err = tx.Exec("DELETE FROM totp_challenges WHERE user_id=?", userID); err != nil {
		return nil, err
	}
	// Every other session of the account ends; the one that made the change stays.
	rows, err := tx.Query(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ? RETURNING token_hash`, userID, TokenHash(keepToken))
	if err != nil {
		return nil, err
	}
	ended, err = sessionHashes(rows)
	if err != nil {
		return nil, err
	}
	return ended, tx.Commit()
}

func sessionHashes(rows *sql.Rows) ([]string, error) {
	var ended []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return nil, err
		}
		ended = append(ended, h)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return ended, nil
}
