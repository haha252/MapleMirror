package storage

import (
	"context"
	"database/sql/driver"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"
)

type timedRows struct {
	inner    driver.Rows
	logger   versionLogFunc
	ctx      context.Context
	query    string
	started  time.Time
	argCount int
	rowCount int64
	once     sync.Once
}

func newTimedRows(rows driver.Rows, logger versionLogFunc, ctx context.Context,
	query string, started time.Time, argCount int) *timedRows {
	return &timedRows{
		inner:    rows,
		logger:   logger,
		ctx:      ctx,
		query:    query,
		started:  started,
		argCount: argCount,
	}
}

func (r *timedRows) Columns() []string {
	return r.inner.Columns()
}

func (r *timedRows) Close() error {
	err := r.inner.Close()
	status := "closed"
	if err != nil {
		status = "close_error"
	}
	r.finish(err, status)
	return err
}

func (r *timedRows) Next(dest []driver.Value) error {
	err := r.inner.Next(dest)
	if err == nil {
		r.rowCount++
		return nil
	}
	if err == io.EOF {
		r.finish(nil, "eof")
		return err
	}
	r.finish(err, "next_error")
	return err
}

func (r *timedRows) finish(err error, status string) {
	r.once.Do(func() {
		logSQLDuration(r.logger, r.ctx, "query", r.query, r.started, r.argCount, r.rowCount, err, status)
	})
}

func (r *timedRows) ColumnTypeDatabaseTypeName(index int) string {
	typed, ok := r.inner.(driver.RowsColumnTypeDatabaseTypeName)
	if !ok {
		return ""
	}
	return typed.ColumnTypeDatabaseTypeName(index)
}

func (r *timedRows) ColumnTypeLength(index int) (int64, bool) {
	typed, ok := r.inner.(driver.RowsColumnTypeLength)
	if !ok {
		return 0, false
	}
	return typed.ColumnTypeLength(index)
}

func (r *timedRows) ColumnTypeNullable(index int) (bool, bool) {
	typed, ok := r.inner.(driver.RowsColumnTypeNullable)
	if !ok {
		return false, false
	}
	return typed.ColumnTypeNullable(index)
}

func (r *timedRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	typed, ok := r.inner.(driver.RowsColumnTypePrecisionScale)
	if !ok {
		return 0, 0, false
	}
	return typed.ColumnTypePrecisionScale(index)
}

func (r *timedRows) ColumnTypeScanType(index int) reflect.Type {
	typed, ok := r.inner.(driver.RowsColumnTypeScanType)
	if !ok {
		return nil
	}
	return typed.ColumnTypeScanType(index)
}

func logSQLDuration(logger versionLogFunc, ctx context.Context, kind, query string, started time.Time,
	argCount int, rowCount int64, err error, status string) {
	if logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("kind", kind),
		slog.String("operation", sqlOperation(query)),
		slog.String("status", status),
		slog.Duration("duration", time.Since(started)),
		slog.Int("arg_count", argCount),
		slog.String("sql", summarizeSQL(query)),
	}
	if rowCount >= 0 {
		attrs = append(attrs, slog.Int64("row_count", rowCount))
	}
	if err != nil && err != io.EOF {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	logger(ctx, "SQL 耗时", attrs...)
}

func sqlOperation(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "UNKNOWN"
	}
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return "UNKNOWN"
	}
	return strings.ToUpper(fields[0])
}

func summarizeSQL(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	query = strings.Join(strings.Fields(query), " ")
	const maxLen = 240
	runes := []rune(query)
	if len(runes) <= maxLen {
		return query
	}
	return string(runes[:maxLen]) + "..."
}
