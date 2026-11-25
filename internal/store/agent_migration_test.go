package store

import (
	"context"
"errors"
	"os"
	"path/filepath"
	"testing"

	"docket/internal/database"
	"docket/internal/testutil"
)

func TestSharedInstallRejectsPreviousLayoutAndPreservesHistory(t *testing.T) {
	var db *database.DB
	var err error
	dir := "migrations"
	if os.Getenv("DOCKET_TEST_POSTGRES_DSN") != "" {
		dir += "/postgres"
		db, err = database.OpenPostgres(testutil.PostgresDSN(t), database.UserSchema(1))
	} else {
		db, err = database.OpenSQLite(filepath.Join(t.TempDir(), "old.db"))
	}
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}
	old, err := migrations.ReadFile(dir + "/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = s.WithTx(ctx, func(tx *Tx) error {
		if err := db.CreateSchema(tx.tx); err != nil {
			return err
		}
		if _, err := tx.tx.Exec(string(old)); err != nil {
			return err
		}
		_, err := tx.tx.Exec(`
			CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '');
			INSERT INTO schema_migrations(version) VALUES ('001_init.sql');
			INSERT INTO projects(slug,key,name,position) VALUES ('old','QA','Old',1);
			INSERT INTO statuses(project_id,key,name,color,category,position) VALUES (1,'todo','Todo','blue','todo',1);
			INSERT INTO tasks(project_id,number,title,status_id,position,created_by) VALUES (1,1,'Parent',1,1,'denis');
			INSERT INTO tasks(project_id,number,parent_id,title,status_id,position,created_by) VALUES (1,2,1,'Legacy',1,2,'claude');
			INSERT INTO notes(task_id,author,body_md) VALUES (2,'denis','existing note');
			INSERT INTO activity(task_id,project_id,actor,action) VALUES (2,1,'claude','created');
		`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.migrate(nil); !errors.Is(err,database.ErrPreviousLayout) {
		t.Fatal(err)
	}
	var parent, creator, author, actor string
	err = db.QueryRow(`SELECT p.created_by, t.created_by, n.author, a.actor FROM tasks t
		JOIN tasks p ON p.id = t.parent_id JOIN notes n ON n.task_id = t.id JOIN activity a ON a.task_id = t.id
		WHERE t.id = 2 AND n.body_md = 'existing note' AND a.action = 'created'`).Scan(&parent, &creator, &author, &actor)
	if err != nil || parent != "denis" || creator != "claude" || author != "denis" || actor != "claude" {
		t.Fatalf("legacy data: %q %q %q %q, %v", parent, creator, author, actor, err)
	}
}
