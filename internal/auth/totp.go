package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"plandillo/internal/database"
)

const factorSchema = `
CREATE TABLE IF NOT EXISTS user_totp(
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret_ciphertext TEXT NOT NULL, key_version INTEGER NOT NULL DEFAULT 1,
 confirmed_at INTEGER, last_used_step INTEGER NOT NULL DEFAULT -1, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS totp_challenges(
 token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at INTEGER NOT NULL, attempts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS totp_user ON totp_challenges(user_id);
CREATE INDEX IF NOT EXISTS totp_expiry ON totp_challenges(expires_at);`

type Enrollment struct {
	Secret string `json:"secret"`
	QR     []byte `json:"qr_png"`
}

func (d *DB) ConfigureTOTP(key []byte) error {
	if len(key) == 0 {
		var active bool
		err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM user_totp WHERE confirmed_at IS NOT NULL)").Scan(&active)
		if err != nil {
			return err
		}
		if active {
			return errors.New("configured accounts require the installation TOTP encryption key")
		}
		return nil
	}
	if len(key) != 32 {
		return errors.New("TOTP encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	d.factorKey, err = cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return err
	}
	var id int64
	var value string
	err = d.db.QueryRow("SELECT user_id,secret_ciphertext FROM user_totp WHERE confirmed_at IS NOT NULL LIMIT 1").Scan(&id, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	if _, err = d.factorKey.Open(nil, nil, ciphertext, factorAAD(id)); err != nil {
		return errors.New("configured TOTP key does not match this installation")
	}
	return nil
}
func (d *DB) TOTPAvailable() bool { return d.factorKey != nil }
func (d *DB) HasTOTP(id int64) (enabled bool, err error) {
	err = d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM user_totp WHERE user_id=? AND confirmed_at IS NOT NULL)", id).Scan(&enabled)
	return
}
func factorAAD(id int64) []byte { return []byte(fmt.Sprintf("plandillo/totp/v1/%d", id)) }
func (d *DB) BeginEnrollment(u User) (Enrollment, error) {
	var out Enrollment
	if d.factorKey == nil {
		return out, ValidationError("configure the installation TOTP key")
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Plandillo", AccountName: u.Username, SecretSize: 20})
	if err != nil {
		return out, err
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return out, err
	}
	var buf bytes.Buffer
	if err = png.Encode(&buf, img); err != nil {
		return out, err
	}
	encrypted := base64.StdEncoding.EncodeToString(d.factorKey.Seal(nil, nil, []byte(key.Secret()), factorAAD(u.ID)))
	res, err := d.db.Exec("INSERT INTO user_totp(user_id,secret_ciphertext,created_at) VALUES (?,?,?) ON CONFLICT(user_id) DO UPDATE SET secret_ciphertext=excluded.secret_ciphertext,created_at=excluded.created_at WHERE user_totp.confirmed_at IS NULL", u.ID, encrypted, d.now().Unix())
	if err != nil {
		return out, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return out, ValidationError("TOTP already enabled")
	}
	return Enrollment{key.Secret(), buf.Bytes()}, nil
}
func (d *DB) verifyStep(tx *database.Tx, id int64, code string, enroll bool) error {
	if d.factorKey == nil || len(code) != 6 {
		return ErrBadCredentials
	}
	var encoded string
	var previous, created int64
	var confirmed sql.NullInt64
	err := tx.QueryRow("SELECT secret_ciphertext,last_used_step,created_at,confirmed_at FROM user_totp WHERE user_id=? AND key_version=1", id).Scan(&encoded, &previous, &created, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBadCredentials
	}
	if err != nil {
		return err
	}
	now := d.now().Unix()
	if confirmed.Valid == enroll || enroll && created+600 <= now {
		return ErrBadCredentials
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	secret, err := d.factorKey.Open(nil, nil, data, factorAAD(id))
	if err != nil {
		return errors.New("cannot open TOTP credential with this installation key")
	}
	for _, offset := range []int64{0, -1, 1} {
		step := now/30 + offset
		if step <= previous {
			continue
		}
		valid, err := totp.ValidateCustom(code, string(secret), time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return err
		}
		if valid {
			res, err := tx.Exec("UPDATE user_totp SET last_used_step=?, confirmed_at=coalesce(confirmed_at,?) WHERE user_id=? AND last_used_step<?", step, now, id, step)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 1 {
				return nil
			}
		}
	}
	return ErrBadCredentials
}
func (d *DB) SetTOTP(ctx context.Context, userID int64, code string, enable bool) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = d.verifyStep(tx, userID, code, enable); err != nil {
		return err
	}
	if !enable {
		if _, err = tx.Exec("DELETE FROM user_totp WHERE user_id=?", userID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM totp_challenges WHERE user_id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) LoginChallenge(ctx context.Context, id int64) (string, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	var active bool
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM user_totp WHERE user_id=? AND confirmed_at IS NOT NULL)", id).Scan(&active); err != nil || !active {
		return "", err
	}
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := d.now().Unix()
	if _, err = tx.Exec("DELETE FROM totp_challenges WHERE user_id=? OR expires_at<=?", id, now); err != nil {
		return "", err
	}
	if _, err = tx.Exec("INSERT INTO totp_challenges(token_hash,user_id,expires_at,attempts) VALUES (?,?,?,5)", hash, id, now+300); err != nil {
		return "", err
	}
	return token, tx.Commit()
}
func (d *DB) ConsumeChallenge(ctx context.Context, token, code string) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	hash := TokenHash(token)
	var id int64
	err = tx.QueryRow("UPDATE totp_challenges SET attempts=attempts-1 WHERE token_hash=? AND expires_at>? AND attempts>0 AND user_id IN (SELECT id FROM users WHERE is_active=1) RETURNING user_id", hash, d.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBadCredentials
	}
	if err != nil {
		return 0, err
	}
	if err = d.verifyStep(tx, id, code, false); err != nil {
		if errors.Is(err, ErrBadCredentials) {
			if e := tx.Commit(); e != nil {
				return 0, e
			}
		}
		return 0, err
	}
	if _, err = tx.Exec("DELETE FROM totp_challenges WHERE token_hash=?", hash); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
