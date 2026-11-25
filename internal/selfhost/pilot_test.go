package selfhost

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"docket/internal/auth"
)

// TestPilot measures reopening all 50 initialized workspaces and an authenticated request for each.
func TestPilot(t *testing.T) {
	if os.Getenv("PLANDILLO_TEST_PILOT") != "1" {
		t.Skip("set PLANDILLO_TEST_PILOT=1 for capacity measurements")
	}
	ctx := context.Background()
	s := newServer(t)
	admin, err := s.db.SetupAdmin("admin", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.SetRegistration(ctx, "open"); err != nil {
		t.Fatal(err)
	}
	users := []auth.User{admin}
	for i := 1; i < auth.PilotCapacity; i++ {
		u, err := s.db.Register(ctx, "", fmt.Sprintf("owner%02d", i), testPassword)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	tokens := make([]string, len(users))
	for i, u := range users {
		ws, err := s.ws.Open(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = bearerFor(t, ws)
	}
	s.ws.Close()
	start := time.Now()
	reopened := NewWorkspaces(ctx, s.ws.dir, time.UTC, s.ws.build)
	reopened.UsePostgres(s.ws.postgresDSN)
	defer reopened.Close()
	if err = reopened.VerifyOwners(ctx, users, s.db); err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if _, err = reopened.Open(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	startup := time.Since(start)
	s.h = Handler(Options{Auth: s.db, WS: reopened, Web: http.NotFoundHandler(), AllowedHosts: hosts})
	var durations []time.Duration
	for _, token := range tokens {
		start = time.Now()
		r := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, token))
		if r.Code != 200 {
			t.Fatalf("MCP returned %d", r.Code)
		}
		durations = append(durations, time.Since(start))
	}
	slices.Sort(durations)
	var size int64
	var conns int
	if s.ws.postgresDSN != "" {
		db, err := sql.Open("pgx", s.ws.postgresDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}()
		err = db.QueryRowContext(ctx, "SELECT pg_database_size(current_database()),count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid!=pg_backend_pid()").Scan(&size, &conns)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		err = filepath.WalkDir(s.ws.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err == nil {
				size += info.Size()
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	t.Logf("workspaces=%d reopen=%s MCP-p50=%s MCP-p95=%s heap-MiB=%.1f storage-MiB=%.1f postgres-connections=%d", len(users), startup, durations[24], durations[47], float64(mem.HeapAlloc)/(1<<20), float64(size)/(1<<20), conns)
}
