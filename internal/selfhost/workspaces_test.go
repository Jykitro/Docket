package selfhost

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/service"
)

func newWS(t *testing.T) (*Workspaces, string) {
	t.Helper()
	dir := t.TempDir()
	w := NewWorkspaces(context.Background(), dir, time.UTC, func(context.Context, *service.Service, *events.Bus) http.Handler { return http.NotFoundHandler() })
	t.Cleanup(w.Close)
	return w, dir
}

func TestOpenCreatesFileAndProfile(t *testing.T) {
	w, dir := newWS(t)
	ctx := context.Background()
	ws, err := w.Open(ctx, auth.User{ID: 7, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "users", "7.db")); err != nil {
		t.Fatal(err)
	}
	if p, _ := ws.Svc.GetProfile(ctx); p.Name != "alice" {
		t.Fatalf("profile = %q", p.Name)
	}
}

func TestOpenIdempotent(t *testing.T) {
	w, _ := newWS(t)
	u := auth.User{ID: 1, Username: "bob"}
	a, err := w.Open(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.Open(context.Background(), u)
	if err != nil || a != b {
		t.Fatalf("expected same workspace, got %p %p (%v)", a, b, err)
	}
}

func TestOpenKeepsExistingProfileName(t *testing.T) {
	w, dir := newWS(t)
	ctx := context.Background()
	u := auth.User{ID: 2, Username: "carol"}
	ws, err := w.Open(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Svc.SetProfile(ctx, "Caroline"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	w2 := NewWorkspaces(context.Background(), dir, time.UTC, func(context.Context, *service.Service, *events.Bus) http.Handler { return http.NotFoundHandler() })
	defer w2.Close()
	ws, err = w2.Open(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ws.Svc.GetProfile(ctx); p.Name != "Caroline" {
		t.Fatalf("profile = %q", p.Name)
	}
}

func TestRemoveDeletesFiles(t *testing.T) {
	w, dir := newWS(t)
	if _, err := w.Open(context.Background(), auth.User{ID: 3, Username: "dave"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove(3); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(filepath.Join(dir, "users", "3.db"+suffix)); !os.IsNotExist(err) {
			t.Fatalf("3.db%s still exists (err=%v)", suffix, err)
		}
	}
	if _, ok := w.Get(3); ok {
		t.Fatal("workspace still registered")
	}
}

func TestRemoveTombstonesAndCancels(t *testing.T) {
	var built context.Context
	w := NewWorkspaces(context.Background(), t.TempDir(), time.UTC, func(ctx context.Context, _ *service.Service, _ *events.Bus) http.Handler {
		built = ctx
		return http.NotFoundHandler()
	})
	t.Cleanup(w.Close)
	u := auth.User{ID: 4, Username: "erin"}
	if _, err := w.Open(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove(4); err != nil {
		t.Fatal(err)
	}
	if built.Err() == nil {
		t.Fatal("router context not cancelled by Remove")
	}
	if _, err := w.Open(context.Background(), u); !errors.Is(err, ErrRemoved) {
		t.Fatalf("Open after Remove: %v, want ErrRemoved", err)
	}
}

func TestCloseCancels(t *testing.T) {
	w, _ := newWS(t)
	ws, err := w.Open(context.Background(), auth.User{ID: 5, Username: "fay"})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if ws.ctx.Err() == nil {
		t.Fatal("workspace context not cancelled by Close")
	}
}
