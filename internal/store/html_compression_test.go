package store

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docket/internal/database"
)

func TestHTMLCompressionBoundsAndIntegrity(t *testing.T) {
	source := strings.Repeat("<p>данные</p>\r\n", 512)
	packed, err := compressRevision(source)
	if err != nil || len(packed) == 0 {
		t.Fatal("large HTML did not compress", err)
	}
	got, err := decompressRevision(string(packed), int64(len(source)))
	if err != nil || got != source {
		t.Fatal("lossless round trip failed", err)
	}
	corrupted := bytes.Clone(packed)
	corrupted[len(corrupted)-8] ^= 1
	for _, test := range []struct {
		payload []byte
		size    int64
	}{
		{corrupted, int64(len(source))}, {packed, int64(len(source) - 1)}, {packed, int64(len(source) + 1)},
		{packed, MaxHTMLBytes + 1}, {append(bytes.Clone(packed), 0), int64(len(source))},
	} {
		if _, err := decompressRevision(string(test.payload), test.size); err == nil {
			t.Fatal("invalid payload accepted")
		}
	}
	randomBytes := make([]byte, 8192)
	rand.New(rand.NewSource(42)).Read(randomBytes)
	for _, v := range []string{"<p>Hello</p>", string(randomBytes)} {
		if p, err := compressRevision(v); err != nil || p != nil {
			t.Fatal("plain fallback", err)
		}
	}
}

func TestSQLiteHTMLRevisionUpgradeAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := database.OpenSQLite(path)
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
	if _, err = tx.Exec(sharedSQLite); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`
 INSERT INTO users(id,username,name,role,created_at,updated_at) VALUES(1,'a','a','user',0,0);
 INSERT INTO workspaces(id,owner_user_id,name,generation,created_at,updated_at) VALUES(1,1,'Workspace','gen',0,0);
 INSERT INTO projects(workspace_id,id,slug,key,name,position) VALUES(1,1,'fixture','QA','Fixture',0);
 INSERT INTO pages(workspace_id,id,project_id,title,title_key,position) VALUES(1,1,1,'Report','report',1);
 INSERT INTO html_documents(id,workspace_id,page_id,title,title_key,version) VALUES(1,1,1,'Report','report',1);
 INSERT INTO html_revisions(workspace_id,document_id,version,author,created_at,html) VALUES(1,1,1,'old','2026-01-01','<p>Legacy</p>');
 UPDATE plandillo_installation SET last_backup_at='2026-01-01';
 `)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	base, err := Shared(db, path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("<p>проверка</p>\n", 1000)
	err = base.ForOwner(1, 1).WithTx(t.Context(), func(tx *Tx) error {
		return tx.SaveDocument(&HTMLDocument{ID: 1, Version: 2, Author: "codex", CreatedAt: "2026-02-01", HTML: body}, "report")
	})
	if err != nil {
		t.Fatal(err)
	}
	var valid bool
	err = db.QueryRow(`SELECT (SELECT version=3 AND last_backup_at='2026-01-01' FROM plandillo_installation)
  AND EXISTS(SELECT 1 FROM html_revisions WHERE version=1 AND html_gzip IS NULL AND html_raw_bytes IS NULL AND html='<p>Legacy</p>')
  AND EXISTS(SELECT 1 FROM html_revisions WHERE version=2 AND html='' AND html_raw_bytes=? AND length(html_gzip)<html_raw_bytes)`, len(body)).Scan(&valid)
	if err != nil || !valid {
		t.Fatal("SQLite revision storage", err)
	}
	backup, err := base.Backup(t.TempDir(), 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	err = restored.WithTx(t.Context(), func(tx *Tx) error {
		for i, want := range []string{"<p>Legacy</p>", body} {
			doc, err := tx.Document(1, i+1)
			if err != nil {
				return err
			}
			if doc.HTML != want {
				t.Errorf("revision %d changed", i+1)
			}
		}
		used, n, err := tx.RevisionUsage(10)
		if n != 2 || used != int64(len(body)+len("<p>Legacy</p>")) {
			t.Errorf("logical quota %d bytes for %d revisions", used, n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
