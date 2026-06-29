package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
	"sync/atomic"
	"time"

	modernsqlite "modernc.org/sqlite"
)

var tracedSQLiteDriverSeq uint64

func sqliteDriverName(opts openOptions) string {
	if opts.sqlDebugLogger == nil {
		return "sqlite"
	}
	name := "mirror-sqlite-" + strconv.FormatUint(atomic.AddUint64(&tracedSQLiteDriverSeq, 1), 10)
	sql.Register(name, &timedSQLiteDriver{
		inner:  &modernsqlite.Driver{},
		logger: opts.sqlDebugLogger,
	})
	return name
}

type timedSQLiteDriver struct {
	inner  driver.Driver
	logger versionLogFunc
}

func (d *timedSQLiteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &timedConn{inner: conn, logger: d.logger}, nil
}

type timedConn struct {
	inner  driver.Conn
	logger versionLogFunc
}

func (c *timedConn) Prepare(query string) (driver.Stmt, error) {
	started := time.Now()
	stmt, err := c.inner.Prepare(query)
	logSQLDuration(c.logger, context.Background(), "prepare", query, started, 0, -1, err, "done")
	if err != nil {
		return nil, err
	}
	return &timedStmt{inner: stmt, logger: c.logger, query: query}, nil
}

func (c *timedConn) Close() error {
	return c.inner.Close()
}

func (c *timedConn) Begin() (driver.Tx, error) {
	started := time.Now()
	tx, err := c.inner.Begin()
	logSQLDuration(c.logger, context.Background(), "begin", "BEGIN", started, 0, -1, err, "done")
	if err != nil {
		return nil, err
	}
	return &timedTx{inner: tx, logger: c.logger}, nil
}

func (c *timedConn) Ping(ctx context.Context) error {
	pinger, ok := c.inner.(driver.Pinger)
	if !ok {
		return driver.ErrSkip
	}
	return pinger.Ping(ctx)
}

func (c *timedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	tx, err := beginner.BeginTx(ctx, opts)
	logSQLDuration(c.logger, ctx, "begin_tx", "BEGIN", started, 0, -1, err, "done")
	if err != nil {
		return nil, err
	}
	return &timedTx{inner: tx, logger: c.logger}, nil
}

func (c *timedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.inner.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}
	started := time.Now()
	stmt, err := preparer.PrepareContext(ctx, query)
	logSQLDuration(c.logger, ctx, "prepare", query, started, 0, -1, err, "done")
	if err != nil {
		return nil, err
	}
	return &timedStmt{inner: stmt, logger: c.logger, query: query}, nil
}

func (c *timedConn) Exec(query string, args []driver.Value) (driver.Result, error) {
	execer, ok := c.inner.(driver.Execer)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	result, err := execer.Exec(query, args)
	logSQLDuration(c.logger, context.Background(), "exec", query, started, len(args), -1, err, "done")
	return result, err
}

func (c *timedConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	queryer, ok := c.inner.(driver.Queryer)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	rows, err := queryer.Query(query, args)
	if err != nil {
		logSQLDuration(c.logger, context.Background(), "query", query, started, len(args), -1, err, "done")
		return nil, err
	}
	return newTimedRows(rows, c.logger, context.Background(), query, started, len(args)), nil
}

func (c *timedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	result, err := execer.ExecContext(ctx, query, args)
	logSQLDuration(c.logger, ctx, "exec", query, started, len(args), -1, err, "done")
	return result, err
}

func (c *timedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		logSQLDuration(c.logger, ctx, "query", query, started, len(args), -1, err, "done")
		return nil, err
	}
	return newTimedRows(rows, c.logger, ctx, query, started, len(args)), nil
}
