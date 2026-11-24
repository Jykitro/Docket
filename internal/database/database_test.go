package database

import (
	"errors"
	"strings"
	"testing"

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
