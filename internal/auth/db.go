package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
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
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
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
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token_hash TEXT NOT NULL UNIQUE,
	created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);`

// DB holds users, sessions and invites in a dedicated SQLite file.
type DB struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth schema: %w", err)
	}
	return &DB{db: db, now: time.Now}, nil
}

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

// createUser validates and inserts inside the caller's transaction.
func (d *DB) createUser(tx *sql.Tx, username, password string, admin bool) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameRe.MatchString(username) {
		return User{}, ValidationError("username must be 2-32 characters: a-z, 0-9, _ . -, starting with a letter or digit")
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	created := d.now().Unix()
	res, err := tx.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`, username, hash, admin, created)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrTaken
		}
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, IsAdmin: admin, CreatedAt: time.Unix(created, 0).UTC().Format(time.RFC3339)}, nil
}

// dummyHash lets Authenticate spend the same time on unknown users as on wrong passwords.
var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword("dummy-password")
	return h
})

func (d *DB) Authenticate(username, password string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var u User
	var hash string
	var created int64
	err := d.db.QueryRow(`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &hash, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		CheckPassword(dummyHash(), password)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !CheckPassword(hash, password) {
		return User{}, ErrBadCredentials
	}
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	return u, nil
}

// NewSession returns a fresh session token; only its SHA-256 is stored.
func (d *DB) NewSession(userID int64) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = d.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hash, userID, d.now().Add(sessionTTL).Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (d *DB) UserBySession(token string) (User, error) {
	var u User
	var created int64
	err := d.db.QueryRow(`SELECT u.id, u.username, u.is_admin, u.created_at FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), d.now().Unix()).Scan(&u.ID, &u.Username, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	return u, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// SetupAdmin creates the first user as admin; the emptiness check and insert share one transaction.
func (d *DB) SetupAdmin(username, password string) (User, error) {
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
	u, err := d.createUser(tx, username, password, true)
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
	_, err = d.db.Exec(`INSERT INTO invites (token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, createdBy, now.Unix(), expires.Unix())
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// AcceptInvite consumes the invite and creates a regular user in one transaction.
func (d *DB) AcceptInvite(token, username, password string) (User, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`SELECT id FROM invites WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashToken(token), d.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidInvite
	}
	if err != nil {
		return User{}, err
	}
	u, err := d.createUser(tx, username, password, false)
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(`UPDATE invites SET used_at = ? WHERE id = ?`, d.now().Unix(), id); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
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
	rows, err := d.db.Query(`SELECT id, username, is_admin, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var created int64
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser removes the user and their sessions.
func (d *DB) DeleteUser(id int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
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
	return tx.Commit()
}

func (d *DB) ChangePassword(userID int64, oldPw, newPw string) error {
	if err := validatePassword(newPw); err != nil {
		return err
	}
	var hash string
	err := d.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !CheckPassword(hash, oldPw) {
		return ErrBadCredentials
	}
	newHash, err := HashPassword(newPw)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, userID)
	return err
}
