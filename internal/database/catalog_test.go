package database

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceSlugs(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "slugs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// A catalog from before slugs: the next start must give each workspace one.
	old := strings.Replace(strings.Replace(catalogDDL, " slug TEXT NOT NULL DEFAULT '',\n", "", 1), "CREATE UNIQUE INDEX workspaces_slug ON workspaces(owner_user_id,slug);\n", "", 1)
	if _, err = tx.Exec(old); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("INSERT INTO users(username,name,role,created_at,updated_at) VALUES ('a','','user',0,0),('b','','user',0,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("INSERT INTO workspaces(owner_user_id,name,generation,created_at,updated_at) VALUES (1,'Работа','g1',0,0),(1,'Работа!','g2',0,0),(2,'Работа','g3',0,0),(1,'Settings','g4',0,0),(1,'★','g5',0,0),(1,'2024','g7',0,0)"); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureCatalog(tx); err != nil {
		t.Fatal(err)
	}
	var got []string
	rows, err := tx.Query("SELECT slug FROM workspaces ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		got = append(got, s)
	}
	rows.Close()
	if want := "rabota rabota-2 rabota space-settings space space-2024";strings.Join(got, " ") != want {
		t.Fatalf("slugs = %q, want %q", got, want)
	}
	if s, err := tx.SpaceSlug(1, "работа"); err != nil || s != "rabota-3" {
		t.Fatalf("next slug = %q, %v", s, err)
	}
	if _, err = tx.Exec("INSERT INTO workspaces(owner_user_id,name,slug,generation,created_at,updated_at) VALUES (1,'x','rabota','g6',0,0)"); err == nil {
		t.Fatal("a duplicate slug of one owner was accepted")
	}
	if err = db.EnsureCatalog(tx); err != nil {
		t.Fatal("second start:", err)
	}
	if got := appliedIDs(t, tx); got != allIDs() {
		t.Fatalf("recorded migrations %q, want %q", got, allIDs())
	}
}

func allIDs() string {
	var ids []string
	for _, m := range migrations {
		ids = append(ids, strconv.Itoa(m.id))
	}
	return strings.Join(ids, ",")
}

func appliedIDs(t *testing.T, tx *Tx) string {
	t.Helper()
	done, err := appliedMigrations(tx.Query)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range migrations {
		if done[m.id] {
			ids = append(ids, strconv.Itoa(m.id))
		}
	}
	return strings.Join(ids, ",")
}

// A fresh install gets the latest layout from the baseline and records every step without running it.
func TestFreshCatalogRecordsMigrations(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = db.EnsureCatalog(tx); err != nil {
		t.Fatal(err)
	}
	if got := appliedIDs(t, tx); got != allIDs() {
		t.Fatalf("recorded %q, want %q", got, allIDs())
	}
}

func TestBackupBeforeMigrations(t *testing.T) {
	dir := t.TempDir()
	path, backups := filepath.Join(dir, "plandillo.db"), filepath.Join(dir, "backups")
	if out, err := BackupBeforeMigrations(path, backups, time.Now()); err != nil || out != "" {
		t.Fatalf("missing file: %q %v", out, err)
	}
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(strings.Replace(catalogDDL, " slug TEXT NOT NULL DEFAULT '',\n", "", 1), "CREATE UNIQUE INDEX workspaces_slug ON workspaces(owner_user_id,slug);\n", "", 1)
	if _, err = db.Exec(old + "INSERT INTO users(username,name,role,created_at,updated_at) VALUES ('a','','user',0,0);INSERT INTO workspaces(owner_user_id,name,generation,created_at,updated_at) VALUES (1,'Old','g',0,0);"); err != nil {
		t.Fatal(err)
	}
	out, err := BackupBeforeMigrations(path, backups, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC))
	if err != nil || filepath.Base(out) != "plandillo-before-migration-20261126-100000.db" {
		t.Fatalf("backup %q %v", out, err)
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup file %v %v", info, err)
	}
	copy, err := OpenSQLite(out)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = copy.QueryRow("SELECT name FROM workspaces").Scan(&name); err != nil || name != "Old" {
		t.Fatalf("backup content %q %v", name, err)
	}
	copy.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureCatalog(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if out, err = BackupBeforeMigrations(path, backups, time.Now()); err != nil || out != "" {
		t.Fatalf("nothing pending, yet %q %v", out, err)
	}
}
