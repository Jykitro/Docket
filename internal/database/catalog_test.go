package database

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"plandillo/internal/testutil"
)

// eachEngine runs a catalog test on SQLite and, with PLANDILLO_TEST_POSTGRES_DSN, on PostgreSQL. The test gets an open
// transaction on an empty database whose search path is the catalog schema.
func eachEngine(t *testing.T, test func(t *testing.T, db *DB, tx *Tx)) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var db *DB
			var err error
			if engine == "sqlite" {
				db, err = OpenSQLite(filepath.Join(t.TempDir(), "catalog.db"))
			} else {
				db, err = OpenPostgres(testutil.PostgresDSN(t), AuthSchema)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = db.CreateSchema(tx); err != nil {
				t.Fatal(err)
			}
			test(t, db, tx)
		})
	}
}

// oldCatalog is the catalog as it was before the numbered migrations (workspace slugs, web push).
func oldCatalog(db *DB) string {
	ddl := strings.Replace(strings.Replace(catalogDDL, " slug TEXT NOT NULL DEFAULT '',\n", "", 1), "CREATE UNIQUE INDEX workspaces_slug ON workspaces(owner_user_id,slug);\n", "", 1)
	ddl = strings.Replace(ddl, pushDDL, "", 1) // migration 2
	return db.catalogDDL(ddl)
}

func TestWorkspaceSlugs(t *testing.T) { eachEngine(t, testWorkspaceSlugs) }
func testWorkspaceSlugs(t *testing.T, db *DB, tx *Tx) {
	// A catalog from before slugs: the next start must give each workspace one.
	if _, err := tx.Exec(oldCatalog(db)); err != nil {
		t.Fatal(err)
	}
	var err error
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
	if want := "rabota rabota-2 rabota space-settings space space-2024"; strings.Join(got, " ") != want {
		t.Fatalf("slugs = %q, want %q", got, want)
	}
	if s, err := tx.SpaceSlug(1, "работа"); err != nil || s != "rabota-3" {
		t.Fatalf("next slug = %q, %v", s, err)
	}
	if err = db.EnsureCatalog(tx); err != nil {
		t.Fatal("second start:", err)
	}
	if got := appliedIDs(t, tx); got != allIDs() {
		t.Fatalf("recorded migrations %q, want %q", got, allIDs())
	}
	if _, err := tx.Exec("INSERT INTO workspaces(owner_user_id,name,slug,generation,created_at,updated_at) VALUES (1,'x','rabota','g6',0,0)"); err == nil {
		t.Fatal("a duplicate slug of one owner was accepted")
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
func TestFreshCatalogRecordsMigrations(t *testing.T) { eachEngine(t, testFreshCatalog) }
func testFreshCatalog(t *testing.T, db *DB, tx *Tx) {
	if err := db.EnsureCatalog(tx); err != nil {
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
	if _, err = db.Exec(oldCatalog(db) + "INSERT INTO users(username,name,role,created_at,updated_at) VALUES ('a','','user',0,0);INSERT INTO workspaces(owner_user_id,name,generation,created_at,updated_at) VALUES (1,'Old','g',0,0);"); err != nil {
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
