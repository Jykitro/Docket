package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Schema changes after the baseline (catalogDDL here, shared_sqlite.sql and shared_postgres.sql in store) are numbered
// steps. Every start applies the missing ones inside the transaction that checks the catalog, so a failed step leaves
// the database as it was. A fresh install already has the latest layout and records every step as applied.
// To change the schema: append a step here and make the same change in the baselines.
var migrations = []migration{
	{1, "workspace slugs", addSpaceSlugs},
	{2, "web push", func(tx *Tx) error { _, err := tx.Exec(tx.db.catalogDDL(pushDDL)); return err }},
}

type migration struct {
	id   int
	name string
	up   func(*Tx) error
}

const migrationsDDL = "CREATE TABLE IF NOT EXISTS plandillo_migrations(id BIGINT PRIMARY KEY, name TEXT NOT NULL, applied_at BIGINT NOT NULL)"

func applyMigrations(tx *Tx, fresh bool) error {
	if _, err := tx.Exec(migrationsDDL); err != nil {
		return err
	}
	done, err := appliedMigrations(tx.Query)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if done[m.id] {
			continue
		}
		if !fresh {
			slog.Info("applying database migration", "id", m.id, "name", m.name)
			if err := m.up(tx); err != nil {
				return fmt.Errorf("migration %d (%s): %w", m.id, m.name, err)
			}
		}
		if _, err := tx.Exec("INSERT INTO plandillo_migrations(id,name,applied_at) VALUES (?,?,?)", m.id, m.name, time.Now().Unix()); err != nil {
			return err
		}
	}
	return nil
}

func appliedMigrations(query func(string, ...any) (*sql.Rows, error)) (map[int]bool, error) {
	rows, err := query("SELECT id FROM plandillo_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		done[id] = true
	}
	return done, rows.Err()
}

// BackupBeforeMigrations copies the SQLite database at path into dir when this start is going to migrate it, so the
// previous layout can be restored. It returns the copy's path, or "" for a new file or one with nothing pending.
func BackupBeforeMigrations(path, dir string, now time.Time) (string, error) {
	if info, err := os.Stat(path); os.IsNotExist(err) || err == nil && info.Size() == 0 {
		return "", nil
	} else if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}).String())
	if err != nil {
		return "", err
	}
	defer db.Close()
	ctx := context.Background()
	var installed, numbered bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='plandillo_installation'),EXISTS(SELECT 1 FROM sqlite_master WHERE name='plandillo_migrations')").Scan(&installed, &numbered); err != nil || !installed {
		return "", err // not a Plandillo database yet: CheckSQLiteLayout and the first start decide
	}
	done := map[int]bool{}
	if numbered {
		if done, err = appliedMigrations(func(q string, args ...any) (*sql.Rows, error) { return db.QueryContext(ctx, q, args...) }); err != nil {
			return "", err
		}
	}
	pending := false
	for _, m := range migrations {
		pending = pending || !done[m.id]
	}
	if !pending {
		return "", nil
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".migration-backup-*") // 0600, like the daily copies
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err = f.Close(); err != nil {
		return "", err
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", f.Name()); err != nil {
		return "", err
	}
	out := filepath.Join(dir, "plandillo-before-migration-"+now.UTC().Format("20060102-150405")+".db")
	if err = os.Link(f.Name(), out); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return out, nil
}

// addSpaceSlugs gives workspaces made before addresses used slugs (/w/<id>/) their slug. The column may already be
// there: a build between the slug change and numbered migrations added it without recording a step.
func addSpaceSlugs(tx *Tx) error {
	q := "SELECT EXISTS(SELECT 1 FROM pragma_table_info('workspaces') WHERE name='slug')"
	if tx.Postgres() {
		q = "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='workspaces' AND column_name='slug')"
	}
	var done bool
	if err := tx.QueryRow(q).Scan(&done); err != nil || done {
		return err
	}
	if _, err := tx.Exec("ALTER TABLE workspaces ADD COLUMN slug TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT id,owner_user_id,name FROM workspaces ORDER BY id")
	if err != nil {
		return err
	}
	type space struct {
		id, owner int64
		name      string
	}
	var all []space
	for rows.Next() {
		var v space
		if err = rows.Scan(&v.id, &v.owner, &v.name); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, v := range all {
		s, err := tx.SpaceSlug(v.owner, v.name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE workspaces SET slug=? WHERE id=?", s, v.id); err != nil {
			return err
		}
	}
	_, err = tx.Exec("CREATE UNIQUE INDEX workspaces_slug ON workspaces(owner_user_id,slug)")
	return err
}
