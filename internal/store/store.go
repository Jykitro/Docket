// Package store owns task storage: opening, migrations, queries and SQLite backups.
package store

import (
	"context"
	"embed"
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
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// OpenPostgres opens one isolated profile schema in a dedicated Docket database.
func OpenPostgres(dsn string, userID int64) (*Store, error) {
	db, err := database.OpenPostgres(dsn, database.UserSchema(userID))
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate PostgreSQL profile: %w", err)
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

func (s *Store) migrate() error {
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
