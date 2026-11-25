package database

import (
	"context"
	"database/sql/driver"
	"strings"

	"modernc.org/sqlite"
)

// SQLite compiles and plans a statement on every prepare, and database/sql prepares a fresh one for each Query or
// Exec on a string. Under load that was about a quarter of the CPU of a read, plus the allocations it drives through
// modernc's single global allocator lock. stmtConnector keeps each connection's statements prepared, so a query
// string compiles once per connection. It sits below database/sql, so it never needs a second pooled connection
// (which would deadlock the single writer).

// maxCachedStmts bounds the statements a connection keeps. Queries past it still run, just without the cache.
const maxCachedStmts = 256

// openSQLiteConnector opens dsn through modernc's connector and wraps its connections with the statement cache.
func openSQLiteConnector(dsn string) (driver.Connector, error) {
	c, err := sqlite.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	return stmtConnector{c}, nil
}

type stmtConnector struct{ driver.Connector }

// sqliteConn is what modernc's connection provides and what the cache forwards.
type sqliteConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

func (c stmtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	inner, ok := conn.(sqliteConn)
	if !ok {
		return conn, nil // a driver without these methods simply runs uncached
	}
	return &stmtConn{sqliteConn: inner, stmts: map[string]*cachedStmt{}}, nil
}

// stmtConn is used by one goroutine at a time (database/sql holds the connection while a call or its rows are
// open), so its map and flags need no lock.
type stmtConn struct {
	sqliteConn
	stmts map[string]*cachedStmt
}

type cachedStmt struct {
	driver.Stmt
	busy bool // its rows are still being read; SQLite cannot run one statement twice at once
}

// cached returns the connection's prepared statement for q, preparing it on first use. It returns nil when q should
// run uncached: a multi-statement script, a full cache, a statement whose rows are still open, or a prepare error
// (the uncached call then reports that error itself).
func (c *stmtConn) cached(ctx context.Context, q string) *cachedStmt {
	if st, ok := c.stmts[q]; ok {
		if st.busy {
			return nil
		}
		return st
	}
	if len(c.stmts) >= maxCachedStmts || strings.Contains(strings.TrimRight(strings.TrimSpace(q), ";"), ";") {
		return nil
	}
	s, err := c.PrepareContext(ctx, q)
	if err != nil {
		return nil
	}
	st := &cachedStmt{Stmt: s}
	c.stmts[q] = st
	return st
}

func (c *stmtConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	st := c.cached(ctx, q)
	if st == nil {
		return c.sqliteConn.QueryContext(ctx, q, args)
	}
	rows, err := st.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		return nil, err
	}
	st.busy = true
	return &cachedRows{Rows: rows, st: st}, nil
}

func (c *stmtConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	st := c.cached(ctx, q)
	if st == nil {
		return c.sqliteConn.ExecContext(ctx, q, args)
	}
	return st.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
}

// Close finalizes the cached statements before the connection itself.
func (c *stmtConn) Close() error {
	for q, st := range c.stmts {
		_ = st.Close() // finalizing cannot lose data; the connection's own Close reports real failures
		delete(c.stmts, q)
	}
	return c.sqliteConn.Close()
}

// cachedRows frees its statement for the next query when closed. It hides modernc's optional column-type methods,
// which only sql.Rows.ColumnTypes uses and this application does not call.
type cachedRows struct {
	driver.Rows
	st *cachedStmt
}

func (r *cachedRows) Close() error {
	r.st.busy = false
	return r.Rows.Close()
}
