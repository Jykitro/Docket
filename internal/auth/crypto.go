// Package auth implements self-host users, sessions and password/token hashing.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// pbkdf2Iter is a var so tests can lower it; the stored hash carries its own count.
var pbkdf2Iter = 600_000

const hashPrefix = "pbkdf2-sha256"

// maxIter bounds the work a stored hash can demand, so a tampered row cannot stall a login.
const maxIter = 10_000_000

// HashPassword returns "pbkdf2-sha256$<iter>$<b64 salt>$<b64 hash>".
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	enc := base64.StdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", hashPrefix, pbkdf2Iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword reports whether pw matches a stored hash; malformed hashes never match.
func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashPrefix {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 || iter > maxIter {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// newToken returns a random URL-safe token and the hex SHA-256 that is stored in its place.
func newToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, TokenHash(token), nil
}

// TokenHash is the hex SHA-256 stored in place of a session or invite token.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
