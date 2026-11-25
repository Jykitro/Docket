// Package selfhost holds the multi-user pieces of the self-hosted server.
package selfhost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"docket/internal/auth"
	"docket/internal/database"
	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
)

// ErrRemoved is returned by Open for a profile that was deleted while the process runs.
var ErrRemoved = errors.New("profile removed")

type workspaceOwnerKey struct{}

// WorkspaceOwner identifies the verified generation for cross-replica worker admission.
func WorkspaceOwner(ctx context.Context) string {
	owner, _ := ctx.Value(workspaceOwnerKey{}).(string)
	return owner
}

// Workspace is one user's own database, service and HTTP router.
type Workspace struct {
	OwnerID     int64
	generation  string
	tokenDigest [sha256.Size]byte // credential state is protected by tokenIndex.mu
	tokenCtx    context.Context
	endToken    context.CancelFunc
	ID          int64
	Svc         *service.Service
	Router      http.Handler
	st          *store.Store
	// ctx ends when the workspace is removed or closed: it stops the router's background work and every
	// request forwarded to it.
	ctx    context.Context
	cancel context.CancelFunc
}

// Workspaces opens one SQLite file or PostgreSQL schema per user.
type Workspaces struct {
	shared      *store.Store
	envToken    string
	firstID     int64
	cluster     bool
	parent      context.Context
	dir         string
	postgresDSN string
	loc         *time.Location
	build       func(context.Context, *service.Service, *events.Bus) http.Handler

	tokens  tokenIndex
	mu      sync.Mutex
	m       map[int64]*Workspace
	removed map[int64]bool // ids are never reused (AUTOINCREMENT), so tombstones are kept for the process life
}

// NewWorkspaces builds each workspace's router with a context derived from parent that ends on Remove or Close.
func NewWorkspaces(parent context.Context, dir string, loc *time.Location,
	build func(context.Context, *service.Service, *events.Bus) http.Handler) *Workspaces {
	return &Workspaces{parent: parent, dir: dir, loc: loc, build: build, m: map[int64]*Workspace{}, removed: map[int64]bool{}}
}

// UsePostgres selects PostgreSQL for every workspace. Configure it before opening any profile.
func (w *Workspaces) UsePostgres(dsn string) { w.postgresDSN = dsn }

func (w *Workspaces) path(id int64) string {
	return filepath.Join(w.dir, "users", fmt.Sprintf("%d.db", id))
}

// Open returns the user's workspace, creating it on first use. On creation the profile name is set to the
// username unless one already exists, so a later rename survives restarts.
func (w *Workspaces) Open(ctx context.Context, u auth.User) (*Workspace, error) {
	if w.shared != nil {
		sp, err := w.shared.Space(ctx, u.ID, 0)
		if err != nil {
			return nil, err
		}
		return w.openSpace(ctx, sp)
	}
	return w.open(ctx, u, true)
}

// Pending deletions must refer to an existing workspace with the same account generation.
func (w *Workspaces) open(ctx context.Context, u auth.User, allowCreate bool) (*Workspace, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if u.WorkspaceKey == "" {
		return nil, fmt.Errorf("workspace %d has no verified catalog generation: follow the legacy upgrade procedure in docs/selfhost-security.md", u.ID)
	}
	if ws, ok := w.m[u.ID]; ok {
		if ws.generation != u.WorkspaceKey {
			return nil, fmt.Errorf("workspace %d belongs to a different account", u.ID)
		}
		if u.DeletionKey != "" {
			err := ws.st.WithTx(ctx, func(tx *store.Tx) error { return verifyDeletion(tx, u) })
			if err != nil {
				return nil, err
			}
		}
		return ws, nil
	}
	if w.removed[u.ID] {
		return nil, ErrRemoved
	}
	st, err := store.OpenWorkspace(w.path(u.ID), w.postgresDSN, u.ID, func(tx *store.Tx, fresh bool) error {
		if fresh && allowCreate {
			if err := tx.SetSetting("selfhost.owner", u.Username); err != nil {
				return err
			}
			return tx.SetSetting("selfhost.generation", u.WorkspaceKey)
		}
		generation, ok, err := tx.Setting("selfhost.generation")
		if err != nil {
			return err
		}
		if !ok || generation != u.WorkspaceKey {
			return fmt.Errorf("workspace %d ownership mismatch: restore matching storage or follow the legacy upgrade procedure in docs/selfhost-security.md", u.ID)
		}
		owner, _, err := tx.Setting("selfhost.owner")
		if err != nil {
			return err
		}
		if owner != u.Username {
			return fmt.Errorf("workspace %d owner name mismatch", u.ID)
		}
		return verifyDeletion(tx, u)
	})
	if err != nil {
		return nil, err
	}
	bus := events.NewBus()
	svc := service.New(st, bus, w.loc)
	if w.cluster {
		svc.UseClusterEvents(u.WorkspaceKey)
	}
	if err := w.init(ctx, svc, u.Username); err != nil {
		return nil, errors.Join(err, st.Close())
	}
	wctx, cancel := context.WithCancel(w.parent)
	ws := &Workspace{ID: u.ID, generation: u.WorkspaceKey, Svc: svc, Router: w.build(context.WithValue(wctx, workspaceOwnerKey{}, u.WorkspaceKey), svc, bus), st: st, ctx: wctx, cancel: cancel}
	if err := svc.ObserveMCPToken(ctx, func(token string) { w.tokens.replace(ws, token) }); err != nil {
		cancel()
		return nil, errors.Join(err, st.Close())
	}
	w.m[u.ID] = ws
	return ws, nil
}

func verifyDeletion(tx *store.Tx, u auth.User) error {
	if u.DeletionKey == "" {
		return nil
	}
	confirmation, _, err := tx.Setting("selfhost.deletion")
	if err != nil {
		return err
	}
	if confirmation != u.DeletionKey {
		return fmt.Errorf("pending deletion %d does not match the restored workspace snapshot", u.ID)
	}
	return nil
}

// Record the individual deletion in the workspace before committing the catalog tombstone.
// A workspace restored from before that request must never be deleted on replay.
func (ws *Workspace) confirmDeletion(ctx context.Context) (string, error) {
	key := rand.Text()
	err := ws.st.WithTx(ctx, func(tx *store.Tx) error { return tx.SetSetting("selfhost.deletion", key) })
	return key, err
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

// Remove closes the user's store, deletes its files or schema and refuses to open the id again.
func (w *Workspaces) Remove(id int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removed[id] = true
	var errs []error
	if ws, ok := w.m[id]; ok {
		ws.cancel()
		w.tokens.replace(ws, "")
		if w.postgresDSN != "" {
			err := ws.st.DeletePostgres()
			closeErr := ws.st.Close()
			delete(w.m, id)
			return errors.Join(err, closeErr)
		}
		errs = append(errs, ws.st.Close())
		delete(w.m, id)
	}
	if w.postgresDSN != "" {
		// An unopened profile can still have a schema left from an earlier run.
		db, err := database.OpenPostgres(w.postgresDSN, database.UserSchema(id))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.DropSchema()
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
		ws.cancel()
		w.tokens.replace(ws, "")
		ws.st.Close()
		delete(w.m, id)
	}
}

// VerifyOwners checks the restored catalog before resuming any owner-confirmed deletions.
func (w *Workspaces) VerifyOwners(ctx context.Context, users []auth.User, db *auth.DB) error {
	ids, err := w.persistedIDs(ctx, db)
	if err != nil {
		return err
	}
	pending, err := db.PendingDeletions(ctx)
	if err != nil {
		return err
	}
	known := make(map[int64]bool, len(users)+len(pending))
	for _, u := range users {
		if u.WorkspaceKey == "" {
			return fmt.Errorf("workspace %d has no verified catalog generation: follow the legacy upgrade procedure in docs/selfhost-security.md", u.ID)
		}
		known[u.ID] = true
	}
	for _, u := range pending {
		if u.WorkspaceKey == "" || u.DeletionKey == "" {
			return fmt.Errorf("pending deletion %d has no verified workspace generation: reconcile the stopped backup before starting Plandillo", u.ID)
		}
		if known[u.ID] {
			return fmt.Errorf("pending deletion conflicts with workspace %d", u.ID)
		}
		known[u.ID] = true
	}
	persisted := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("workspace %d has no account: restore matching auth and workspace storage before starting Plandillo", id)
		}
		persisted[id] = true
	}
	// A mismatch anywhere in the backup stops all pending deletions.
	for _, u := range users {
		if persisted[u.ID] {
			if _, err = w.open(ctx, u, false); err != nil {
				return err
			}
		}
	}
	// Verify every pending owner before deleting any storage. A missing marker requires operator reconciliation.
	for _, u := range pending {
		if persisted[u.ID] {
			if _, err = w.open(ctx, u, false); err != nil {
				return err
			}
		}
	}
	for _, u := range pending {
		if persisted[u.ID] {
			if err = w.Remove(u.ID); err != nil {
				return err
			}
		}
		if err = db.FinishDeletion(ctx, u.ID); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspaces) persistedIDs(ctx context.Context, db *auth.DB) ([]int64, error) {
	if w.postgresDSN != "" {
		return db.WorkspaceSchemas(ctx)
	}
	files, err := os.ReadDir(filepath.Join(w.dir, "users"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, f := range files {
		name, suffix, found := strings.Cut(f.Name(), ".db")
		if !found || (suffix != "" && suffix != "-wal" && suffix != "-shm") {
			continue
		}
		if id, e := strconv.ParseInt(name, 10, 64); e == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
