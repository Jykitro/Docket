// Package testutil creates disposable PostgreSQL databases for integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// PostgresDSN requires an explicit test-only server whose role can create databases.
// Every call creates a fresh database; cleanup never touches the supplied database.
func PostgresDSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv("PLANDILLO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PLANDILLO_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test PostgreSQL DSN")
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot connect to test PostgreSQL")
	}
	name := fmt.Sprintf("docket_test_%x", rand.Text()[:16])
	if _, err = conn.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Error(err)
		}
	})
	// pgx.Config.ConnString returns the original input, even after Config.Database changes.
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("PLANDILLO_TEST_POSTGRES_DSN must be a postgres:// or postgresql:// URL")
	}
	u.Path = "/" + name
	q := u.Query()
	q.Del("dbname")
	u.RawQuery = q.Encode()
	return u.String()
}
