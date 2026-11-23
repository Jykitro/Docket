package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docket/internal/store"
)

func TestStorageMaintenance(t *testing.T) {
	s := newSvc(t)
	_, err := s.EnsureMCPSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Old log entries must be purged on both backends, independently of file backups.
	err = s.st.WithTx(ctx, func(tx *store.Tx) error {
		return tx.InsertCall(store.MCPCall{At: time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339), Tool: "search", Status: "ok", Actor: ActorClaude})
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	path, err := s.Maintenance(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.DBHealth(ctx)
	if err != nil || h.QuickCheck != "ok" {
		t.Fatalf("health=%v err=%v", h, err)
	}
	if os.Getenv("DOCKET_TEST_POSTGRES_DSN") != "" {
		if path != "" || h.LastBackupAt != "" || h.Journal != "postgresql" {
			t.Fatalf("PostgreSQL reports fictitious backup: path=%q health=%v", path, h)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("unexpected backup directory: %v", err)
		}
	} else if path == "" || h.LastBackupAt == "" || h.Journal != "wal" {
		t.Fatalf("SQLite backup missing: path=%q health=%v", path, h)
	}
	err = s.st.WithTx(ctx, func(tx *store.Tx) error {
		calls, err := tx.ListCalls("all", nil, 100)
		if err == nil && len(calls) != 0 {
			t.Errorf("old calls survived maintenance: %v", calls)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStorageSearchUpdatesAndLongCodes(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	x := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "Розробка dashboard", DescriptionMD: "Проверка платежей"})[0]
	for _, q := range []string{"розроб", "dash", "Розробка dash"} {
		r, err := s.Search(ctx, q, "tasks", 10)
		if err != nil || r.Counts["tasks"] != 1 {
			t.Fatalf("query=%q result=%+v err=%v", q, r, err)
		}
	}
	title := "Changed title"
	if _, err := s.UpdateTask(ctx, ActorDenis, x.Code, Changes{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Search(ctx, "розроб", "tasks", 10); err != nil || r.Counts["tasks"] != 0 {
		t.Fatalf("stale search entry: %v %v", r, err)
	}
	if rows, err := s.ListTasks(ctx, store.TaskFilter{Q: "---", IncludeDone: true}); err != nil || len(rows) != 0 {
		t.Fatalf("punctuation query=%v err=%v", rows, err)
	}
	if err := s.st.WithTx(ctx, func(tx *store.Tx) error {
		row, err := tx.GetTask(x.ID)
		if err != nil {
			return err
		}
		row.Number = 123
		return tx.UpdateTask(row)
	}); err != nil {
		t.Fatal(err)
	}
	row, err := s.GetTask(ctx, p.Key+"-123", 1)
	if err != nil || !strings.HasSuffix(row.Code, "-123") {
		t.Fatalf("long task number=%v err=%v", row, err)
	}
}
