// Package selfhost holds the multi-user pieces of the self-hosted server.
package selfhost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
)

// Workspace is one user's own database, service and HTTP router.
type Workspace struct {
	ID     int64
	Svc    *service.Service
	Router http.Handler
	st     *store.Store
}

// Workspaces lazily opens one SQLite file per user at <dir>/users/<id>.db.
type Workspaces struct {
	dir   string
	loc   *time.Location
	build func(*service.Service, *events.Bus) http.Handler

	mu sync.Mutex
	m  map[int64]*Workspace
}

func NewWorkspaces(dir string, loc *time.Location, build func(*service.Service, *events.Bus) http.Handler) *Workspaces {
	return &Workspaces{dir: dir, loc: loc, build: build, m: map[int64]*Workspace{}}
}

func (w *Workspaces) path(id int64) string {
	return filepath.Join(w.dir, "users", fmt.Sprintf("%d.db", id))
}

// Open returns the user's workspace, creating it on first use. On creation the profile name is set to the
// username unless one already exists, so a later rename survives restarts.
func (w *Workspaces) Open(ctx context.Context, u auth.User) (*Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ws, ok := w.m[u.ID]; ok {
		return ws, nil
	}
	st, err := store.Open(w.path(u.ID))
	if err != nil {
		return nil, err
	}
	bus := events.NewBus()
	svc := service.New(st, bus, w.loc)
	if err := w.init(ctx, svc, u.Username); err != nil {
		st.Close()
		return nil, err
	}
	ws := &Workspace{ID: u.ID, Svc: svc, Router: w.build(svc, bus), st: st}
	w.m[u.ID] = ws
	return ws, nil
}

func (w *Workspaces) init(ctx context.Context, svc *service.Service, username string) error {
	// The env token is deliberately not set: a shared token would make MCP routing ambiguous between users.
	if _, err := svc.EnsureMCPSettings(ctx); err != nil {
		return err
	}
	p, err := svc.GetProfile(ctx)
	if err != nil {
		return err
	}
	if p.Name == "" {
		_, err = svc.SetProfile(ctx, username)
	}
	return err
}

func (w *Workspaces) Get(id int64) (*Workspace, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ws, ok := w.m[id]
	return ws, ok
}

func (w *Workspaces) All() []*Workspace {
	w.mu.Lock()
	defer w.mu.Unlock()
	all := make([]*Workspace, 0, len(w.m))
	for _, ws := range w.m {
		all = append(all, ws)
	}
	return all
}

// Remove closes the user's store and deletes the database files.
func (w *Workspaces) Remove(id int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var errs []error
	if ws, ok := w.m[id]; ok {
		errs = append(errs, ws.st.Close())
		delete(w.m, id)
	}
	p := w.path(id)
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes every open store.
func (w *Workspaces) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, ws := range w.m {
		ws.st.Close()
		delete(w.m, id)
	}
}
