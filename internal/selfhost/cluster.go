// PostgreSQL coordination for replicas of the Vercel selfhost service.
package selfhost

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"docket/internal/auth"
	"docket/internal/database"
)

//go:embed cluster.sql
var clusterSchema string

type Cluster struct {
	sweepMu   sync.Mutex
	nextSweep time.Time
	DB        *sql.DB
	gates     *sql.DB // requests holding access locks must never exhaust the coordination pool
	mu        sync.Mutex
	epoch     int64
}

func OpenCluster(ctx context.Context, dsn string) (*Cluster, error) {
	db, err := database.OpenPostgres(dsn, "docket_auth")
	if err != nil {
		return nil, err
	}
	gates, err := database.OpenPostgres(dsn, "docket_auth")
	if err != nil {
		db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(8)
	gates.SetMaxOpenConns(8)
	c := &Cluster{DB: db.DB, gates: gates.DB, epoch: -1}
	tx, err := c.guard(ctx, true)
	if err == nil {
		defer tx.release()
		_, err = tx.ExecContext(ctx, clusterSchema)
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *Cluster) Close() { c.DB.Close(); c.gates.Close() }

type clusterGate struct {
	*sql.Tx
	conn *sql.Conn
}

func (g *clusterGate) release() { g.Rollback(); g.conn.Close() }
func (c *Cluster) guard(ctx context.Context, exclusive bool) (*clusterGate, error) {
	conn, err := c.gates.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	query := "SELECT pg_advisory_xact_lock_shared(720403250)"
	if exclusive {
		query = "SELECT pg_advisory_xact_lock(720403250)"
	}
	if _, err = tx.ExecContext(ctx, query); err != nil {
		tx.Rollback()
		conn.Close()
		return nil, err
	}
	return &clusterGate{Tx: tx, conn: conn}, nil
}

// Persist the invalidation before any independently committed account or token change.
func (c *Cluster) invalidate(ctx context.Context) error {
	_, err := c.DB.ExecContext(ctx, "UPDATE vercel_state SET epoch=epoch+1 WHERE singleton")
	return err
}
func (c *Cluster) Initialize(ctx context.Context, ws *Workspaces, ad *auth.DB) error {
	ws.cluster = true
	tx, err := c.guard(ctx, true)
	if err != nil {
		return err
	}
	defer tx.release()
	if err = c.invalidate(ctx); err != nil {
		return err
	}
	users, err := ad.ListUsers()
	if err != nil {
		return err
	}
	if err = ws.VerifyOwners(ctx, users, ad); err != nil {
		return err
	}
	return c.sync(ctx, ws, ad)
}
func (c *Cluster) sync(ctx context.Context, ws *Workspaces, ad *auth.DB) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var epoch int64
	if err := c.DB.QueryRowContext(ctx, "SELECT epoch FROM vercel_state WHERE singleton").Scan(&epoch); err != nil {
		return err
	}
	if epoch == c.epoch {
		return nil
	}
	users, err := ad.ListUsers()
	if err != nil {
		return err
	}
	active := make(map[int64]bool, len(users))
	for _, u := range users {
		w, err := ws.Open(ctx, u)
		if err != nil {
			return err
		}
		tok, err := w.Svc.MCPToken(ctx)
		if err != nil {
			return err
		}
		ws.tokens.replace(w, tok)
		active[u.ID] = true
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for id, w := range ws.m {
		if !active[id] {
			w.cancel()
			ws.tokens.replace(w, "")
			w.st.Close()
			delete(ws.m, id)
		}
	}
	c.epoch = epoch
	return nil
}
func (c *Cluster) middleware(h *handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/healthz" || path == "/api/events" && r.Method == http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
			defer cancel()
			writes := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
			exclusive := writes && (strings.HasPrefix(path, "/api/auth/") || strings.HasPrefix(path, "/api/admin/") || path == "/api/mcp/token/rotate")
			// Bound body reads before taking a cluster-wide access transition lock.
			if exclusive {
				ctl := http.NewResponseController(w)
				_ = ctl.SetReadDeadline(time.Now().Add(10 * time.Second))
				limit := int64(8192)
				if strings.HasPrefix(path, "/api/auth/passkeys/") && strings.HasSuffix(path, "/finish") {
					limit = 64 << 10
				}
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
				_ = ctl.SetReadDeadline(time.Time{})
				if err != nil {
					writeError(w, 400, "validation", "invalid credentials body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			tx, err := c.guard(ctx, exclusive)
			if err != nil {
				writeError(w, 503, "unavailable", "storage unavailable")
				return
			}
			defer tx.release()
			if err = c.sync(ctx, h.WS, h.Auth); err != nil {
				internalError(w, err)
				return
			}
			if err = c.maintain(ctx, h.WS); err != nil {
				internalError(w, err)
				return
			}
			catalogChange := path == "/api/auth/register" || path == "/api/auth/setup" || path == "/api/auth/recover" || path == "/api/auth/account" || strings.HasPrefix(path, "/api/auth/invite/") || strings.HasPrefix(path, "/api/admin/users/") || path == "/api/mcp/token/rotate"
			if exclusive && catalogChange {
				if err = c.invalidate(ctx); err != nil {
					internalError(w, err)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// A stream holds the shared access lock only while checking its session and sending a batch.
// Revocation on any replica therefore fences every subsequent event.
func (c *Cluster) stream(h *handler, w http.ResponseWriter, r *http.Request) {
	ctl := http.NewResponseController(w)
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	var cursor int64
	first := true
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		err := func() error {
			tx, err := c.guard(ctx, false)
			if err != nil {
				return err
			}
			defer tx.release()
			user, ok, err := h.session(r)
			if err != nil {
				return err
			}
			if !ok {
				if first {
					writeError(w, 401, "unauthorized", "not logged in")
				}
				return auth.ErrNotFound
			}
			if first {
				if !matchesWorkspace(w, r, user) {
					return auth.ErrNotFound
				}
				if err = c.DB.QueryRowContext(ctx, "SELECT COALESCE(max(id),0) FROM vercel_events WHERE owner=$1", user.WorkspaceKey).Scan(&cursor); err != nil {
					return err
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("X-Accel-Buffering", "no")
				first = false
				fmt.Fprint(w, ": connected\n\n")
				return ctl.Flush()
			}
			rows, err := c.DB.QueryContext(ctx, "SELECT id,event FROM vercel_events WHERE owner=$1 AND id>$2 ORDER BY id LIMIT 128", user.WorkspaceKey, cursor)
			if err != nil {
				return err
			}
			defer rows.Close()
			_ = ctl.SetWriteDeadline(time.Now().Add(3 * time.Second))
			defer ctl.SetWriteDeadline(time.Time{})
			for rows.Next() {
				var data []byte
				if err = rows.Scan(&cursor, &data); err != nil {
					return err
				}
				if _, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", cursor, data); err != nil {
					return err
				}
			}
			if err = rows.Err(); err != nil {
				return err
			}
			fmt.Fprint(w, ": ping\n\n")
			return ctl.Flush()
		}()
		if err != nil {
			if first && !errors.Is(err, auth.ErrNotFound) {
				writeError(w, 503, "unavailable", "event stream unavailable")
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reclaim expired transient state during requests; replicas that are frozen need no timer.
// The database elects one replica per interval, and rollback leaves the sweep retryable.
func (c *Cluster) maintain(ctx context.Context, ws *Workspaces) error {
	c.sweepMu.Lock()
	if time.Now().Before(c.nextSweep) {
		c.sweepMu.Unlock()
		return nil
	}
	c.nextSweep = time.Now().Add(time.Minute)
	c.sweepMu.Unlock()
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE vercel_state SET cleaned=clock_timestamp() WHERE singleton AND (cleaned IS NULL OR cleaned<clock_timestamp()-interval '10 minutes')")
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	for _, query := range []string{
		"DELETE FROM vercel_jobs WHERE expires<=clock_timestamp()",
		"DELETE FROM vercel_flows WHERE expires<=clock_timestamp()",
		"DELETE FROM vercel_attempts WHERE expires<=clock_timestamp()",
		"DELETE FROM vercel_events WHERE created<clock_timestamp()-interval '10 minutes'",
	} {
		if _, err = tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	for _, w := range ws.All() {
		if _, err = w.Svc.PurgeOldMCPCalls(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}
