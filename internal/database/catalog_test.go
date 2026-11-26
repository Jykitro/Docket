package database

import (
	"path/filepath"
	"strings"
	"testing"
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
}
