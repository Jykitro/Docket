// Package store owns task storage: opening, migrations, queries and SQLite backups.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"docket/internal/database"
)

//go:embed migrations/*.sql migrations/postgres/*.sql
var migrations embed.FS

type Store struct {
	db   *database.DB
	path string
}

func Open(path string) (*Store, error) {
	db, err := database.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	return openStore(db, path, nil)
}

func OpenPostgres(dsn string, userID int64) (*Store, error) {
	db, err := database.OpenPostgres(dsn, database.UserSchema(userID))
	if err != nil {
		return nil, err
	}
	return openStore(db, "", nil)
}

// OpenWorkspace verifies ownership in the migration transaction before exposing any workspace data.
// A new database binds its owner in the same commit that creates its tables.
func OpenWorkspace(path, dsn string, userID int64, verify func(*Tx, bool) error) (*Store, error) {
	var db *database.DB
	var err error
	if dsn != "" {
		db, err = database.OpenPostgres(dsn, database.UserSchema(userID))
	} else {
		db, err = database.OpenSQLite(path)
	}
	if err != nil {
		return nil, err
	}
	return openStore(db, path, verify)
}

func openStore(db *database.DB, path string, verify func(*Tx, bool) error) (*Store, error) {
	s := &Store{db: db, path: path}
	if err := s.migrate(verify); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// DeletePostgres removes this profile's schema without affecting other profiles.
func (s *Store) DeletePostgres() error { return s.db.DropSchema() }

func (s *Store) Close() error { return s.db.Close() }

// Health runs PRAGMA quick_check and reports the journal mode and the database file size in bytes.
func (s *Store) Health() (quick, journal string, size int64, err error) {
	if s.db.Postgres {
		var n int
		if err = s.db.QueryRow(`SELECT 1`).Scan(&n); err != nil {
			return
		}
		err = s.db.QueryRow(`SELECT coalesce(sum(pg_total_relation_size(c.oid)),0)::bigint FROM pg_class c
   JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind='r'`).Scan(&size)
		return "ok", "postgresql", size, err
	}

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

func (s *Store) migrate(verify func(*Tx, bool) error) error {
	dir := "migrations"
	if s.db.Postgres {
		dir += "/postgres"
	}
	files, err := migrations.ReadDir(dir)
	if err != nil {
		return err
	}
	// The schema lock and all migrations share a transaction: concurrent starts cannot partially
	// initialize a schema, and failed migrations roll back the schema as well.
	return s.WithTx(context.Background(), func(tx *Tx) error {
		if err := s.db.CreateSchema(tx.tx); err != nil {
			return err
		}
		if _, err := tx.tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
			return err
		}
		newWorkspace := false
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			var n int
			if err := tx.tx.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = ?`, f.Name()).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			if f.Name() == "001_init.sql" {
				newWorkspace = true
			}
			body, err := migrations.ReadFile(dir + "/" + f.Name())
			if err != nil {
				return err
			}
			if _, err := tx.tx.Exec(string(body)); err != nil {
				return fmt.Errorf("%s: %w", f.Name(), err)
			}
			if _, err := tx.tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, f.Name()); err != nil {
				return err
			}
		}
		if verify != nil {
			return verify(tx, newWorkspace)
		}
		return nil
	})
}

// Tx is the only way to run queries; every service operation is one transaction.
type Tx struct{ tx *database.Tx }

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
	if s.db.Postgres {
		return "", nil
	} // PostgreSQL backups belong to the operator (pg_dump/PITR).

	if keep < 1 {
		return "", fmt.Errorf("backup retention must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "docket-"+now.Format("20060102-1504")+".db")
	if err := database.PrivateFile(path, false); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".backup-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err = f.Close(); err != nil {
		return "", err
	}
	if _, err = s.db.Exec("VACUUM INTO ?", f.Name()); err != nil {
		return "", err
	}
	if err = os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if err = database.PrivateFile(path, false); err != nil {
			return "", err
		}
	}
	old, err := filepath.Glob(filepath.Join(dir, "docket-*.db"))
	if err != nil {
		return path, err
	}
	sort.Strings(old) // timestamped names sort chronologically
	for len(old) > keep {
		if err := os.Remove(old[0]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return path, err
		}
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

// RecordClusterEvent is part of the same serialized profile transaction as the change.
func (tx *Tx) RecordClusterEvent(owner string, event []byte) error {
	_, err := tx.tx.Exec("INSERT INTO docket_auth.vercel_events(owner,event) VALUES(?,?::json)", owner, string(event))
	return err
}
