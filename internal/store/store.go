// Package store owns task storage: opening, migrations, queries and SQLite backups.
package store

import (
	"context"
	"crypto/rand"
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
	db          *database.DB
	path        string
	ownerID     int64
	workspaceID int64
	borrowed    bool
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

func (s *Store) Close() error {
	if s.borrowed {
		return nil
	}
	return s.db.Close()
}

// Health runs PRAGMA quick_check and reports the journal mode and the database file size in bytes.
func (s *Store) Health() (quick, journal string, size int64, err error) {
	if s.db.Postgres {
		var n int
		if err = s.db.QueryRow(`SELECT 1`).Scan(&n); err != nil {
			return
		}
		if s.ownerID != 0 {
			err = s.db.QueryRow("SELECT content_bytes FROM workspaces WHERE id=? AND owner_user_id=?", s.workspaceID, s.ownerID).Scan(&size)
			return "ok", "postgresql", size, err
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
	if s.ownerID == 0 {
		if fi, e := os.Stat(s.path); e == nil {
			size = fi.Size()
		}
	}
	return
}

//go:embed shared_sqlite.sql
var sharedSQLite string

//go:embed shared_postgres.sql
var sharedPostgres string

//go:embed sqlite_html_revisions.sql
var sqliteHTMLRevisions string

//go:embed read_indexes.sql
var readIndexes string

// Shared opens the planner tables on the same database as the account catalog.
func Shared(db *database.DB, path string) (*Store, error) {
	s := &Store{db: db, path: path, borrowed: true}
	return s, s.initialize(nil, false)
}
func (s *Store) ForOwner(id, owner int64) *Store {
	st := s.ForWorkspace(id)
	st.ownerID = owner
	return st
}
func (s *Store) ForWorkspace(id int64) *Store {
	return &Store{db: s.db, path: s.path, borrowed: true, workspaceID: id}
}

func (s *Store) migrate(verify func(*Tx, bool) error) error { return s.initialize(verify, true) }
func (s *Store) initialize(verify func(*Tx, bool) error, standalone bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.db.CreateSchema(tx); err != nil {
		return err
	}
	if err = s.db.EnsureCatalog(tx); err != nil {
		return err
	}
	var exists bool
	q := "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='projects')"
	if s.db.Postgres {
		q = "SELECT to_regclass('projects') IS NOT NULL"
	}
	if err = tx.QueryRow(q).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		body := sharedSQLite
		if s.db.Postgres {
			body = sharedPostgres
		}
		if _, err = tx.Exec(body); err != nil {
			return err
		}
	}
	if !s.db.Postgres {
		var version int
		if err = tx.QueryRow("SELECT version FROM plandillo_installation").Scan(&version); err != nil {
			return err
		}
		if version == 2 {
			if _, err = tx.Exec(sqliteHTMLRevisions); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(readIndexes); err != nil {
		return err
	}
	if _, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_pages_wiki_candidates ON pages(workspace_id,project_id,position,id) WHERE " + wikiMarker(s.db.Postgres)); err != nil {
		return err
	}
	if standalone {
		if !exists {
			now := time.Now().Unix()
			var uid int64
			if err = tx.QueryRow("INSERT INTO users(username,name,role,created_at,updated_at) VALUES ('local','','user',?,?) RETURNING id", now, now).Scan(&uid); err != nil {
				return err
			}
			if err = tx.QueryRow("INSERT INTO workspaces(owner_user_id,name,generation,created_at,updated_at) VALUES (?,'Personal',?,?,?) RETURNING id", uid, rand.Text(), now, now).Scan(&s.workspaceID); err != nil {
				return err
			}
		} else if err = tx.QueryRow("SELECT id FROM workspaces ORDER BY id LIMIT 1").Scan(&s.workspaceID); err != nil {
			return err
		}
		tx.BindWorkspace(s.workspaceID)
		if verify != nil {
			if err = verify(&Tx{tx: tx}, !exists); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Tx is the only way to run queries; every service operation is one transaction.
type Tx struct {
	tx        *database.Tx
	scopedURL bool
}

func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, s.db.BeginOwnerTx, fn)
}

// WithReadTx is WithTx for operations that only read: it runs on the read pool in parallel with other readers
// (database.DB.BeginReadTx), and SQLite rejects any write attempted inside it.
func (s *Store) WithReadTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, s.db.BeginReadTx, fn)
}

func (s *Store) withTx(ctx context.Context, begin func(context.Context, int64) (*database.Tx, error), fn func(*Tx) error) error {
	tx, err := begin(ctx, s.ownerID)
	if err != nil {
		return err
	}
	tx.BindWorkspace(s.workspaceID)
	if s.ownerID != 0 {
		var active bool
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM workspaces w JOIN users u ON u.id=w.owner_user_id WHERE w.id=:workspace AND w.owner_user_id=? AND w.archived_at IS NULL AND u.is_active=1)", s.ownerID).Scan(&active); err != nil || !active {
			tx.Rollback()
			if err != nil {
				return err
			}
			return ErrNotFound
		}
	}
	if err := fn(&Tx{tx: tx, scopedURL: s.ownerID != 0}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// BackupKeep is how many automatic SQLite snapshots the server retains.
const BackupKeep = 14

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

// PurgeExpiredCalls applies the retention policy to the whole installation, including unopened spaces.
func (s *Store) PurgeExpiredCalls(ctx context.Context, now time.Time) error {
	cutoff := now.UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	_, err := s.db.ExecContext(ctx, s.db.SQL("DELETE FROM mcp_calls WHERE at < ?"), cutoff)
	return err
}

func (s *Store) RecordBackup(at time.Time) error {
	_, err := s.db.Exec("UPDATE plandillo_installation SET last_backup_at=?", at.UTC().Format("2006-01-02T15:04:05.000Z"))
	return err
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
