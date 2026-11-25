package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PostgreSQL replicas share the operator-provided key. Native SQLite uses one private
// key file alongside the database. Publish a completed file atomically on first boot.
func totpKey(dir string, postgres bool) ([]byte, error) {
	if value := os.Getenv("PLANDILLO_TOTP_KEY"); value != "" {
		key, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(key) != 32 {
			return nil, errors.New("PLANDILLO_TOTP_KEY must be a base64-encoded 32-byte key")
		}
		return key, nil
	}
	if postgres {
		return nil, nil
	}
	path := filepath.Join(dir, "totp.key")
	read := func() ([]byte, error) {
		st, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("TOTP key requires a private regular file")
		}
		return os.ReadFile(path)
	}
	key, err := read()
	if !os.IsNotExist(err) {
		return key, err
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, ".totp-key-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(key)
	if err = errors.Join(err, file.Close()); err != nil {
		return nil, err
	}
	if err = os.Link(file.Name(), path); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return read()
}
