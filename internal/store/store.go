// Package store owns the SQLite database: opening, migrations, queries, backups.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writes; a single-user app never needs more.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Health runs PRAGMA quick_check and reports the journal mode and the database file size in bytes.
func (s *Store) Health() (quick, journal string, size int64, err error) {
	if err = s.db.QueryRow(`PRAGMA quick_check`).Scan(&quick); err != nil {
		return
	}
	if err = s.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		return
	}
	if fi, e := os.Stat(s.path); e == nil {
		size = fi.Size()
	}
	return
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, f := range files { // ReadDir returns entries sorted by name
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, f.Name()).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		err = s.WithTx(context.Background(), func(tx *Tx) error {
			if _, err := tx.tx.Exec(string(body)); err != nil {
				return fmt.Errorf("%s: %w", f.Name(), err)
			}
			_, err := tx.tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, f.Name())
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Tx is the only way to run queries; every service operation is one transaction.
type Tx struct{ tx *sql.Tx }

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Tx{tx: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Backup writes a VACUUM INTO copy named docket-YYYYMMDD-HHMM.db and keeps the newest `keep` copies.
func (s *Store) Backup(dir string, keep int, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "docket-"+now.Format("20060102-1504")+".db")
	if _, err := os.Stat(path); err == nil {
		return path, nil // already backed up this minute
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", err
	}
	old, err := filepath.Glob(filepath.Join(dir, "docket-*.db"))
	if err != nil {
		return path, err
	}
	sort.Strings(old) // timestamped names sort chronologically
	for len(old) > keep {
		os.Remove(old[0])
		old = old[1:]
	}
	return path, nil
}

// ftsQuery turns free text into a safe FTS5 prefix query: "foo"* "bar"*.
func ftsQuery(q string) string {
	var parts []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, `""`)
		parts = append(parts, `"`+w+`"*`)
	}
	return strings.Join(parts, " ")
}
