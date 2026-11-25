// Package database provides the small SQL dialect boundary shared by task and auth storage.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	Postgres bool
	schema   string
}

type Tx struct {
	*sql.Tx
	db          *DB
	workspaceID int64
}

func OpenSQLite(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := PrivateFile(path, true); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-shm", "-wal", "-journal"} {
		if err := PrivateFile(path+suffix, false); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// The SQLite URI must not interpret punctuation in a filename as connection parameters.
	name := filepath.ToSlash(absolute)
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	uri := url.URL{Scheme: "file", Path: name}
	db, err := sql.Open("sqlite", uri.String()+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &DB{DB: db}, nil
}

// PrivateFile prepares a regular application-owned data file before SQLite opens it.
func PrivateFile(path string, create bool) error {
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err == nil {
			return f.Close()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("expected a regular data file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, info) {
		return fmt.Errorf("data file changed: %s", path)
	}
	if info.Mode().Perm()&0077 != 0 {
		return f.Chmod(0600)
	}
	return nil
}

const AuthSchema = "plandillo_auth"

var schemaName = regexp.MustCompile(`^plandillo_(auth|user_[1-9][0-9]*)$`)
var errSchemaLayout = errors.New("conflicting Plandillo schemas or application tables in public; reconcile the dedicated database before startup")

func resolveSchema(ctx context.Context, conn *pgx.Conn, name string) (string, error) {
	var current, legacy, misplaced bool
	err := conn.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname ~ '^plandillo_(auth|user_[1-9][0-9]*)$'),
		EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname ~ '^docket_(auth|user_[1-9][0-9]*)$'),
		(pg_catalog.to_regclass('public.workspace_bindings') IS NOT NULL AND pg_catalog.to_regclass('public.users') IS NOT NULL)
		OR (pg_catalog.to_regclass('public.schema_migrations') IS NOT NULL AND pg_catalog.to_regclass('public.settings') IS NOT NULL)`).Scan(&current, &legacy, &misplaced)
	if err != nil {
		return "", err
	}
	if misplaced || (current && legacy) {
		return "", errSchemaLayout
	}
	if legacy {
		return "docket_" + strings.TrimPrefix(name, "plandillo_"), nil
	}
	return name, nil
}

// OpenPostgres pins search_path on every connection, including reconnects. No public-schema fallback
// is allowed: a missing profile schema must never resolve another profile's tables.
func OpenPostgres(dsn, schema string) (*DB, error) {
	if !schemaName.MatchString(schema) {
		return nil, errors.New("invalid Plandillo schema")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid PLANDILLO_POSTGRES_DSN")
	} // parser errors can contain credentials
	cfg.ConnectTimeout = 10 * time.Second
	// Some managed endpoints ignore startup search_path. Set it in SQL on every
	// physical connection, using the same schema family throughout this pool.
	var detect sync.Once
	var physical string
	var layoutErr error
	raw := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		detect.Do(func() { physical, layoutErr = resolveSchema(ctx, conn, schema) })
		if layoutErr != nil {
			return layoutErr
		}
		_, err := conn.Exec(ctx, "SELECT pg_catalog.set_config('search_path', $1, false)", physical)
		return err
	}))
	raw.SetMaxOpenConns(1) // preserve the existing serialized per-profile transaction model
	raw.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := raw.PingContext(ctx); err != nil {
		raw.Close()
		if errors.Is(err, errSchemaLayout) {
			return nil, errSchemaLayout
		}
		return nil, errors.New("cannot connect to PostgreSQL; check PLANDILLO_POSTGRES_DSN and server availability")
	}
	return &DB{DB: raw, Postgres: true, schema: physical}, nil
}

func UserSchema(id int64) string { return "plandillo_user_" + strconv.FormatInt(id, 10) }

// WorkspaceSchemas enumerates generated tenant schemas in the dedicated PostgreSQL database.
func (d *DB) WorkspaceSchemas(ctx context.Context) ([]int64, error) {
	prefix, _, _ := strings.Cut(d.schema, "_")
	rows, err := d.QueryContext(ctx, "SELECT split_part(nspname, '_', 3)::bigint FROM pg_catalog.pg_namespace WHERE nspname ~ $1", "^"+prefix+"_user_[1-9][0-9]*$")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // read-only cursor; iteration errors are returned below
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Catalog transitions exclude workspace transactions. Normal planner operations take
// a shared catalog lock and serialize only with other operations for the same owner.
func (d *DB) Begin() (*Tx, error) { return d.BeginTx(context.Background(), nil) }
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	return d.begin(ctx, opts, 0)
}
func (d *DB) BeginOwnerTx(ctx context.Context, owner int64) (*Tx, error) {
	return d.begin(ctx, nil, owner)
}
func (d *DB) begin(ctx context.Context, opts *sql.TxOptions, owner int64) (*Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if d.Postgres {
		lock := "SELECT pg_advisory_xact_lock(hashtextextended($1,0))"
		if owner > 0 {
			lock = "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,0))"
		}
		if _, err = tx.ExecContext(ctx, lock, d.schema); err == nil && owner > 0 {
			_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,1))", d.schema+"/"+strconv.FormatInt(owner, 10))
		}
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return &Tx{Tx: tx, db: d}, nil
}

func (d *DB) CreateSchema(tx *Tx) error {
	if !d.Postgres {
		return nil
	}
	_, err := tx.Exec(`CREATE SCHEMA IF NOT EXISTS ` + pgx.Identifier{d.schema}.Sanitize())
	return err
}

// DropSchema deletes only this generated profile schema. The auth schema cannot be dropped here.
func (d *DB) DropSchema() error {
	if !d.Postgres || strings.HasSuffix(d.schema, "_auth") {
		return errors.New("not a PostgreSQL profile")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DROP SCHEMA IF EXISTS ` + pgx.Identifier{d.schema}.Sanitize() + ` CASCADE`); err != nil {
		return err
	}
	return tx.Commit()
}

func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" || err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// SQL adapts the handful of SQLite expressions used by the shared queries. Values are always
// parameters; quoted question marks stay literal. Dialect-specific search/JSON remains in store.
func (d *DB) SQL(q string) string {
	if !d.Postgres {
		return q
	}
	q = strings.ReplaceAll(q, "strftime('%Y-%m-%dT%H:%M:%fZ','now')", `to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')`)
	for _, col := range []string{"t.number", "pt.number"} {
		q = strings.ReplaceAll(q, "printf('%02d', "+col+")", "lpad("+col+"::text, greatest(2, length("+col+"::text)), '0')")
	}
	return numberedParameters(q, '$')
}

func numberedParameters(q string, prefix byte) string {
	var b strings.Builder
	quote := byte(0)
	n := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == quote {
				if i+1 < len(q) && q[i+1] == quote {
					i++
					b.WriteByte(q[i])
				} else {
					quote = 0
				}
			}
		} else if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
		} else if c == '?' {
			n++
			b.WriteByte(prefix)
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Boolean flags are integer columns in both backends to keep the existing storage contract.
func argsForSQL(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		if v, ok := a.(bool); ok {
			if v {
				out[i] = 1
			} else {
				out[i] = 0
			}
		} else {
			out[i] = a
		}
	}
	return out
}

func (d *DB) Exec(q string, args ...any) (sql.Result, error) {
	return d.DB.Exec(d.SQL(q), argsForSQL(args)...)
}
func (d *DB) Query(q string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(d.SQL(q), argsForSQL(args)...)
}
func (d *DB) QueryRow(q string, args ...any) *sql.Row {
	return d.DB.QueryRow(d.SQL(q), argsForSQL(args)...)
}

func (t *Tx) WorkspaceID() int64 { return t.workspaceID }

// BindWorkspace pins domain statements to the workspace selected by the router.
func (t *Tx) BindWorkspace(id int64) { t.workspaceID = id }
func (t *Tx) statement(q string, args []any) (string, []any) {
	q = t.db.SQL(q)
	args = argsForSQL(args)
	if strings.Contains(q, ":workspace") {
		if t.workspaceID <= 0 {
			panic("workspace query outside scoped transaction")
		}
		if t.db.Postgres {
			q = strings.ReplaceAll(q, ":workspace", "$"+strconv.Itoa(len(args)+1))
			args = append(args, t.workspaceID)
		} else {
			q = numberedParameters(q, '?')
			q = strings.ReplaceAll(q, ":workspace", "?"+strconv.Itoa(len(args)+1))
			args = append(args, t.workspaceID)
		}
	}
	return q, args
}
func (t *Tx) Exec(q string, args ...any) (sql.Result, error) {
	q, args = t.statement(q, args)
	return t.Tx.Exec(q, args...)
}
func (t *Tx) Query(q string, args ...any) (*sql.Rows, error) {
	q, args = t.statement(q, args)
	return t.Tx.Query(q, args...)
}
func (t *Tx) QueryRow(q string, args ...any) *sql.Row {
	q, args = t.statement(q, args)
	return t.Tx.QueryRow(q, args...)
}
func (t *Tx) Postgres() bool { return t.db.Postgres }

// AuthSchema returns the catalog paired with this transaction's workspace.
func (t *Tx) AuthSchema() string {
	prefix, _, _ := strings.Cut(t.db.schema, "_")
	return pgx.Identifier{prefix + "_auth"}.Sanitize()
}
