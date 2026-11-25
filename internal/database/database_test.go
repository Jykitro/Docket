package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docket/internal/testutil"
	"github.com/jackc/pgx/v5"
)

func TestSQLParametersAndLiterals(t *testing.T) {
	q := `SELECT ?, '?', 'it''s ?', "what?", ?`
	if got := (&DB{Postgres: true}).SQL(q); got != `SELECT $1, '?', 'it''s ?', "what?", $2` {
		t.Fatalf("SQL = %s", got)
	}
	if got := (&DB{}).SQL(q); got != q {
		t.Fatalf("SQLite SQL changed: %s", got)
	}
}

// The SQLite read pool must reject writes, see committed data, and not wait for an open write transaction.
func TestSQLiteReadPool(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err = db.Exec("CREATE TABLE t(v INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"rejects writes", func(t *testing.T) {
			tx, err := db.BeginReadTx(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.Exec("INSERT INTO t VALUES (2)"); err == nil {
				t.Fatal("a write inside a read transaction succeeded")
			}
		}},
		{"sees committed data", func(t *testing.T) {
			var n int
			if err := db.ReadQueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&n); err != nil || n != 1 {
				t.Fatalf("count = %d, %v", n, err)
			}
		}},
		{"runs beside an open write", func(t *testing.T) {
			w, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = w.Rollback() }()
			if _, err = w.Exec("INSERT INTO t VALUES (3)"); err != nil {
				t.Fatal(err)
			}
			read, cancel := context.WithTimeout(ctx, time.Second) // the single writer connection is busy
			defer cancel()
			var n int
			if err := db.ReadQueryRowContext(read, "SELECT count(*) FROM t").Scan(&n); err != nil || n != 1 {
				t.Fatalf("read beside a writer: count = %d, %v (want the committed 1, without waiting)", n, err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// SetPool must keep every connection it allows open: the reuse is the point.
func TestSetPoolKeepsConnectionsWarm(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetPool(6)
	ctx := context.Background()
	conns := make([]*sql.Conn, 6)
	for i := range conns { // hold six at once, then hand them all back
		if conns[i], err = db.Conn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if s := db.Stats(); s.MaxOpenConnections != 6 || s.Idle != 6 || s.MaxIdleClosed != 0 {
		t.Fatalf("max open %d, idle %d, closed for idle limit %d; want 6, 6, 0", s.MaxOpenConnections, s.Idle, s.MaxIdleClosed)
	}
}

// The statement cache must give the same answers as uncached SQL in the cases where reusing a statement could go wrong.
func TestSQLiteStatementCache(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("CREATE TABLE t(v INTEGER); INSERT INTO t VALUES (1),(2),(3)"); err != nil {
		t.Fatal("a multi-statement script must still run:", err)
	}
	count := func(t *testing.T, q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"new bindings on reuse", func(t *testing.T) {
			for _, tc := range []struct{ min, want int }{{1, 3}, {2, 2}, {3, 1}, {4, 0}, {0, 3}} {
				if got := count(t, "SELECT count(*) FROM t WHERE v >= ?", tc.min); got != tc.want {
					t.Fatalf("v >= %d: %d, want %d", tc.min, got, tc.want)
				}
			}
		}},
		{"nested identical query on one connection", func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			q := "SELECT v FROM t ORDER BY v"
			outer, err := tx.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = outer.Close() }()
			seen := 0
			for outer.Next() {
				inner, err := tx.Query(q) // the cached statement is busy: this must fall back, not reset outer
				if err != nil {
					t.Fatal(err)
				}
				n := 0
				for inner.Next() {
					n++
				}
				_ = inner.Close()
				if n != 3 {
					t.Fatalf("inner saw %d rows, want 3", n)
				}
				seen++
			}
			if seen != 3 {
				t.Fatalf("outer saw %d rows, want 3 (the inner query reset it)", seen)
			}
		}},
		{"schema change after caching", func(t *testing.T) {
			if got := count(t, "SELECT count(*) FROM pragma_table_info('t')"); got != 1 {
				t.Fatalf("columns before: %d", got)
			}
			if _, err := db.Exec("ALTER TABLE t ADD COLUMN w TEXT"); err != nil {
				t.Fatal(err)
			}
			if got := count(t, "SELECT count(*) FROM pragma_table_info('t')"); got != 2 {
				t.Fatalf("columns after: %d (a stale cached plan)", got)
			}
		}},
		{"more distinct queries than the cache holds", func(t *testing.T) {
			for i := range maxCachedStmts + 20 {
				if got := count(t, fmt.Sprintf("SELECT count(*) + %d FROM t", i)); got != 3+i {
					t.Fatalf("query %d: %d", i, got)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestPostgresSchemaFamily(t *testing.T) {
	for _, tt := range []struct{ name, setup, family string }{
		{"new", "", "plandillo"},
		{"legacy", "CREATE SCHEMA docket_auth", "docket"},
		{"mixed", "CREATE SCHEMA docket_auth; CREATE SCHEMA plandillo_auth", ""},
		{"public", "CREATE TABLE public.users(id int); CREATE TABLE public.workspace_bindings(id int)", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dsn := testutil.PostgresDSN(t)
			ctx := t.Context()
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal("open test database")
			}
			defer conn.Close(ctx)
			if tt.setup != "" {
				if _, err := conn.Exec(ctx, tt.setup); err != nil {
					t.Fatal(err)
				}
			}
			profile, err := OpenPostgres(dsn, UserSchema(42))
			if tt.family == "" {
				if !errors.Is(err, errSchemaLayout) {
					t.Fatalf("expected layout error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer profile.Close()
			tx, err := profile.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := profile.CreateSchema(tx); err != nil {
				t.Fatal(err)
			}
			if tx.AuthSchema() != (pgx.Identifier{tt.family + "_auth"}).Sanitize() {
				t.Fatal("wrong event catalog")
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			profile.SetMaxIdleConns(0)
			for i := 0; i < 2; i++ {
				var got string
				if err := profile.QueryRow("SELECT current_schema()").Scan(&got); err != nil || got != tt.family+"_user_42" {
					t.Fatalf("schema after reconnect = %q, err %v", got, err)
				}
			}
			catalog, err := OpenPostgres(dsn, AuthSchema)
			if err != nil {
				t.Fatal(err)
			}
			defer catalog.Close()
			ids, err := catalog.WorkspaceSchemas(ctx)
			if err != nil || len(ids) != 1 || ids[0] != 42 {
				t.Fatalf("workspace schemas %v, %v", ids, err)
			}
		})
	}
}

func TestSQLTimestampsAndTaskNumbers(t *testing.T) {
	q := (&DB{Postgres: true}).SQL(`SELECT printf('%02d', t.number), printf('%02d', pt.number), strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`)
	for _, bad := range []string{"printf", "strftime", "?"} {
		if strings.Contains(q, bad) {
			t.Fatalf("SQLite expression left: %s", q)
		}
	}
	if !strings.Contains(q, "greatest(2, length(t.number::text))") || !strings.Contains(q, "$1") {
		t.Fatalf("SQL = %s", q)
	}
}

func TestPostgresRejectsSchemaAndRedactsDSN(t *testing.T) {
	if _, err := OpenPostgres("", `plandillo_user_1; DROP SCHEMA public`); err == nil {
		t.Fatal("accepted invalid schema")
	}
	_, err := OpenPostgres("postgres://user:secret@host:bad/db", AuthSchema)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe parse error: %v", err)
	}
}
