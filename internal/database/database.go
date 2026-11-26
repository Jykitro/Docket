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
	"runtime"
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

	// SQLite only: the same file opened query_only for parallel readers (see BeginReadTx).
	readURI  string
	readOnce sync.Once
	reader   *sql.DB
	readErr  error
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
	conn, err := openSQLiteConnector(uri.String() + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(conn)
	db.SetMaxOpenConns(1) // SQLite has one writer; queueing here is cheaper than SQLITE_BUSY retries
	return &DB{DB: db, readURI: uri.String() + "?_pragma=query_only(1)&_pragma=busy_timeout(5000)"}, nil
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

// Schemas from the Docket name are no longer opened; starting anyway would begin with empty plandillo_ schemas.
var errRetiredSchemas = errors.New("docket_ schemas found: rename each to plandillo_ (ALTER SCHEMA docket_auth RENAME TO plandillo_auth, and the same for every docket_user_N) before startup")

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
	if legacy {
		return "", errRetiredSchemas
	}
	if misplaced {
		return "", errSchemaLayout
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
		for _, known := range []error{errSchemaLayout, errRetiredSchemas} { // the bare message, never the DSN
			if errors.Is(err, known) {
				return nil, known
			}
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

// BeginReadTx starts a read-only transaction that runs in parallel with other readers. On SQLite it uses a separate
// query_only pool, so in WAL mode readers neither queue behind the single writer nor behind each other. On PostgreSQL
// it is READ ONLY and takes the owner lock shared: readers of one owner overlap, and still wait for a writer that
// holds the exclusive lock, so a read never observes the middle of that owner's write sequence.
func (d *DB) BeginReadTx(ctx context.Context, owner int64) (*Tx, error) {
	if !d.Postgres {
		r, err := d.readers()
		if err != nil {
			return nil, err
		}
		tx, err := r.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		return &Tx{Tx: tx, db: d}, nil
	}
	tx, err := d.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,0))", d.schema)
	if err == nil && owner > 0 {
		_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,1))", d.schema+"/"+strconv.FormatInt(owner, 10))
	}
	if err != nil {
		_ = tx.Rollback() // the lock query failed; its error is the one to report
		return nil, err
	}
	return &Tx{Tx: tx, db: d}, nil
}

// SetPool sizes a pool shared by many requests. All n connections stay open between transactions, so the next
// request reuses a connected, authenticated session with warm statement caches instead of opening a new one (the
// default keeps only two idle and reconnects the rest after every burst). Connections idle for 5 minutes still
// close, and each is replaced after 30 minutes so a database restart or failover is picked up gradually.
func (d *DB) SetPool(n int) {
	d.SetMaxOpenConns(n)
	d.SetMaxIdleConns(n) // after SetMaxOpenConns, which caps it
	d.SetConnMaxIdleTime(5 * time.Minute)
	d.SetConnMaxLifetime(30 * time.Minute)
}

// ReadQueryRowContext runs a single read outside a transaction on the read pool (see BeginReadTx).
func (d *DB) ReadQueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	r, err := d.readers()
	if err != nil {
		r = d.DB // the writer can always answer a read; the read pool only adds parallelism
	}
	return r.QueryRowContext(ctx, d.SQL(q), argsForSQL(args)...)
}

// ReadQueryContext is ReadQueryRowContext for several rows.
func (d *DB) ReadQueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	r, err := d.readers()
	if err != nil {
		r = d.DB
	}
	return r.QueryContext(ctx, d.SQL(q), argsForSQL(args)...)
}

// readers opens the SQLite read pool on first use, so short-lived handles (migrations, checks) never pay for it.
// PostgreSQL reads share the main pool.
func (d *DB) readers() (*sql.DB, error) {
	if d.readURI == "" {
		return d.DB, nil
	}
	d.readOnce.Do(func() {
		conn, err := openSQLiteConnector(d.readURI)
		if err != nil {
			d.readErr = fmt.Errorf("open SQLite read pool: %w", err)
			return
		}
		r := sql.OpenDB(conn)
		n := max(4, runtime.GOMAXPROCS(0))
		r.SetMaxOpenConns(n)
		r.SetMaxIdleConns(n)
		d.reader = r
	})
	return d.reader, d.readErr
}

// Close closes the read pool, if one was opened, and then the main connection.
func (d *DB) Close() error {
	d.readOnce.Do(func() {}) // waits for an opening in flight and stops later ones
	if d.reader != nil {
		_ = d.reader.Close() // read-only connections hold nothing to flush; the writer's error is the one that matters
	}
	return d.DB.Close()
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
