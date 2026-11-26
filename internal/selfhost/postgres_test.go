package selfhost

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"plandillo/internal/auth"
	"plandillo/internal/events"
	"plandillo/internal/service"
	"plandillo/internal/testutil"
)

func TestPostgresWorkspacePersistenceAndRemoval(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	dir := t.TempDir()
	build := func(context.Context, *service.Service, *events.Bus) http.Handler { return http.NotFoundHandler() }
	w := NewWorkspaces(context.Background(), dir, time.UTC, build)
	w.UsePostgres(dsn)
	defer w.Close()
	ctx := context.Background()
	alice, err := w.Open(ctx, auth.User{ID: 1, Username: "alice", WorkspaceKey: "fixture-alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := w.Open(ctx, auth.User{ID: 2, Username: "bob", WorkspaceKey: "fixture-bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = alice.Svc.SetProfile(ctx, "Alice renamed"); err != nil {
		t.Fatal(err)
	}
	if p, err := bob.Svc.GetProfile(ctx); err != nil || p.Name != "bob" {
		t.Fatalf("profile leaked: %v %v", p, err)
	}
	w.Close()
	// Reopen through new pools to verify migrations and search_path after reconnects.
	alice, err = w.Open(ctx, auth.User{ID: 1, Username: "alice", WorkspaceKey: "fixture-alice"})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := alice.Svc.GetProfile(ctx); err != nil || p.Name != "Alice renamed" {
		t.Fatalf("profile lost: %v %v", p, err)
	}
	// Also remove a persisted schema that this Workspaces instance never opened.
	if err = w.Remove(2); err != nil {
		t.Fatal(err)
	}
	if err = w.Remove(1); err != nil {
		t.Fatal(err)
	}
	fresh := NewWorkspaces(context.Background(), dir, time.UTC, build)
	fresh.UsePostgres(dsn)
	defer fresh.Close()
	for _, id := range []int64{1, 2} {
		ws, err := fresh.Open(ctx, auth.User{ID: id, Username: "fresh", WorkspaceKey: "fixture-fresh"})
		if err != nil {
			t.Fatal(err)
		}
		if p, err := ws.Svc.GetProfile(ctx); err != nil || p.Name != "fresh" {
			t.Fatalf("removed data survived: %v %v", p, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("PostgreSQL created local files: %v %v", entries, err)
	}
}
