package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

func (d *DB) UpdateProfile(id int64, name string) error {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return ValidationError("name must be 1–40 characters")
	}
	_, err := d.db.Exec("UPDATE users SET name=?,updated_at=? WHERE id=? AND is_active=1", name, d.now().Unix(), id)
	return err
}
func (d *DB) Photo(id int64) ([]byte, error) {
	var data []byte
	err := d.db.QueryRow("SELECT photo FROM users WHERE id=? AND photo IS NOT NULL", id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return data, err
}
func (d *DB) SetPhoto(id int64, data []byte) error {
	_, err := d.db.Exec("UPDATE users SET photo=?,updated_at=? WHERE id=?", data, d.now().Unix(), id)
	return err
}

type DeviceSession struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func (d *DB) Devices(ctx context.Context, userID int64, cookie string) ([]DeviceSession, error) {
	rows, err := d.db.QueryContext(ctx, d.db.SQL("SELECT id,user_agent,created_at,last_seen_at,expires_at,token_hash=? FROM sessions WHERE user_id=? AND expires_at>? ORDER BY created_at DESC,id"), TokenHash(cookie), userID, d.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceSession{}
	for rows.Next() {
		var s DeviceSession
		var created, seen, expires int64
		if err = rows.Scan(&s.ID, &s.UserAgent, &created, &seen, &expires, &s.Current); err != nil {
			return nil, err
		}
		s.CreatedAt = time.Unix(created, 0).UTC()
		s.LastSeenAt = time.Unix(seen, 0).UTC()
		s.ExpiresAt = time.Unix(expires, 0).UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}
func (d *DB) RevokeDevice(userID int64, id string) (string, error) {
	var hash string
	err := d.db.QueryRow("DELETE FROM sessions WHERE user_id=? AND id=? RETURNING token_hash", userID, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return hash, err
}

func (d *DB) ByID(id int64) (User, error) {
	var u User
	var created, updated int64
	err := d.db.QueryRow("SELECT id,username,name,role,is_admin,is_active,created_at,updated_at,CASE WHEN photo IS NOT NULL THEN '/api/users/' || CAST(id AS TEXT) || '/photo' END,coalesce((SELECT generation FROM workspace_bindings WHERE user_id=users.id AND pending=0),'') FROM users WHERE id=?", id).Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.IsAdmin, &u.IsActive, &created, &updated, &u.Photo, &u.WorkspaceKey)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	u.CreatedAt = time.Unix(created, 0).UTC().Format(time.RFC3339)
	u.UpdatedAt = time.Unix(updated, 0).UTC().Format(time.RFC3339)
	u.Blocked = !u.IsActive
	return u, err
}

// PinMode prevents opening a multi-user database through the unauthenticated local router.
func (d *DB) PinMode(mode string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS installation_mode (id INTEGER PRIMARY KEY CHECK(id=1),mode TEXT NOT NULL)"); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO installation_mode(id,mode) VALUES(1,?) ON CONFLICT(id) DO NOTHING", mode); err != nil {
		return err
	}
	var stored string
	if err = tx.QueryRow("SELECT mode FROM installation_mode WHERE id=1").Scan(&stored); err != nil {
		return err
	}
	if mode != stored {
		return errors.New("database belongs to a different Plandillo installation mode")
	}
	return tx.Commit()
}
