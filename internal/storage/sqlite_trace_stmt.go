package storage

import (
	"context"
	"database/sql/driver"
	"time"
)

type timedStmt struct {
	inner  driver.Stmt
	logger versionLogFunc
	query  string
}

func (s *timedStmt) Close() error {
	return s.inner.Close()
}

func (s *timedStmt) NumInput() int {
	return s.inner.NumInput()
}

func (s *timedStmt) Exec(args []driver.Value) (driver.Result, error) {
	started := time.Now()
	result, err := s.inner.Exec(args)
	logSQLDuration(s.logger, context.Background(), "stmt_exec", s.query, started, len(args), -1, err, "done")
	return result, err
}

func (s *timedStmt) Query(args []driver.Value) (driver.Rows, error) {
	started := time.Now()
	rows, err := s.inner.Query(args)
	if err != nil {
		logSQLDuration(s.logger, context.Background(), "stmt_query", s.query, started, len(args), -1, err, "done")
		return nil, err
	}
	return newTimedRows(rows, s.logger, context.Background(), s.query, started, len(args)), nil
}

func (s *timedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	result, err := execer.ExecContext(ctx, args)
	logSQLDuration(s.logger, ctx, "stmt_exec", s.query, started, len(args), -1, err, "done")
	return result, err
}

func (s *timedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		logSQLDuration(s.logger, ctx, "stmt_query", s.query, started, len(args), -1, err, "done")
		return nil, err
	}
	return newTimedRows(rows, s.logger, ctx, s.query, started, len(args)), nil
}

type timedTx struct {
	inner  driver.Tx
	logger versionLogFunc
}

func (t *timedTx) Commit() error {
	started := time.Now()
	err := t.inner.Commit()
	logSQLDuration(t.logger, context.Background(), "commit", "COMMIT", started, 0, -1, err, "done")
	return err
}

func (t *timedTx) Rollback() error {
	started := time.Now()
	err := t.inner.Rollback()
	logSQLDuration(t.logger, context.Background(), "rollback", "ROLLBACK", started, 0, -1, err, "done")
	return err
}
