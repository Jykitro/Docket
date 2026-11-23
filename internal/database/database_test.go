package database

import (
	"strings"
	"testing"
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
	if _, err := OpenPostgres("", `docket_user_1; DROP SCHEMA public`); err == nil {
		t.Fatal("accepted invalid schema")
	}
	_, err := OpenPostgres("postgres://user:secret@host:bad/db", "docket_auth")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe parse error: %v", err)
	}
}
