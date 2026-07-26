package storage

import (
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/config"
)

type lifecycleConn struct {
	valid      bool
	resetCalls int
	resetErr   error
}

func (*lifecycleConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*lifecycleConn) Close() error                        { return nil }
func (*lifecycleConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c *lifecycleConn) IsValid() bool                     { return c.valid }
func (c *lifecycleConn) ResetSession(context.Context) error {
	c.resetCalls++
	return c.resetErr
}

type recordedSQLLog struct {
	message string
	attrs   map[string]string
}

func TestSQLDebugConnectionRecoversAfterCanceledQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace-cancel.db")
	wal := true
	db, err := OpenMaster(config.Database{
		Path: path, BusyTimeout: "5s", WAL: &wal,
		WALAutocheckpointPages: 1_000_000,
	},
		WithSQLDebugLogger(func(context.Context, string, ...slog.Attr) {}))
	if err != nil {
		t.Fatalf("OpenMaster failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE cancel_query_values (value INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create cancellation fixture: %v", err)
	}
	if _, err := db.Exec(`
		WITH RECURSIVE numbers(value) AS (
			VALUES(0)
			UNION ALL
			SELECT value + 1 FROM numbers WHERE value < 10000
		)
		INSERT INTO cancel_query_values(value) SELECT value FROM numbers
	`); err != nil {
		t.Fatalf("seed cancellation fixture: %v", err)
	}
	if result, err := runWALCheckpoint(db, path, "TRUNCATE"); err != nil {
		t.Fatalf("truncate WAL before canceled query: %v", err)
	} else if result.Busy != 0 || incompleteWALCheckpoint(result) {
		t.Fatalf("initial checkpoint incomplete: %+v", result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	var sum int64
	err = db.QueryRowContext(ctx, `
		WITH RECURSIVE numbers(value) AS (
			VALUES(0)
			UNION ALL
			SELECT value + 1 FROM numbers WHERE value < 100000000
		)
		SELECT sum(numbers.value * cancel_query_values.value)
		FROM numbers CROSS JOIN cancel_query_values
	`).Scan(&sum)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("long query error = %v, want context deadline exceeded", err)
	}

	if _, err := db.Exec(`INSERT INTO cancel_query_values(value) VALUES (10001)`); err != nil {
		t.Fatalf("write after canceled query: %v", err)
	}
	result, err := runWALCheckpoint(db, path, "PASSIVE")
	if err != nil {
		t.Fatalf("checkpoint after canceled query failed: %v", err)
	}
	if result.Busy != 0 || incompleteWALCheckpoint(result) {
		t.Fatalf("canceled query pinned WAL snapshot: %+v", result)
	}

	if err := db.QueryRow(`SELECT 1`).Scan(&sum); err != nil {
		t.Fatalf("query after canceled query failed: %v", err)
	}
	if sum != 1 {
		t.Fatalf("query after canceled query returned %d, want 1", sum)
	}
}

func TestTimedConnForwardsConnectionLifecycle(t *testing.T) {
	wantErr := errors.New("reset failed")
	inner := &lifecycleConn{resetErr: wantErr}
	conn := &timedConn{inner: inner}

	if conn.IsValid() {
		t.Fatal("IsValid returned true for invalid inner connection")
	}
	if err := conn.ResetSession(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ResetSession error = %v, want %v", err, wantErr)
	}
	if inner.resetCalls != 1 {
		t.Fatalf("ResetSession calls = %d, want 1", inner.resetCalls)
	}
}

type sqlLogRecorder struct {
	mu      sync.Mutex
	entries []recordedSQLLog
}

func (r *sqlLogRecorder) Log(_ context.Context, message string, attrs ...slog.Attr) {
	entry := recordedSQLLog{message: message, attrs: make(map[string]string, len(attrs))}
	for _, attr := range attrs {
		entry.attrs[attr.Key] = attr.Value.String()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

func (r *sqlLogRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

func (r *sqlLogRecorder) FindBySQL(sql string) (recordedSQLLog, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		if entry.attrs["sql"] == sql {
			return entry, true
		}
	}
	return recordedSQLLog{}, false
}

func (r *sqlLogRecorder) Snapshot() []recordedSQLLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedSQLLog, len(r.entries))
	copy(out, r.entries)
	return out
}

func TestOpenMasterSQLDebugLoggerLogsExecAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	recorder := &sqlLogRecorder{}
	db, err := OpenMaster(config.Database{Path: path, BusyTimeout: "5s"},
		WithSQLDebugLogger(recorder.Log))
	if err != nil {
		t.Fatalf("OpenMaster failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	recorder.Reset()

	if _, err := db.Exec(`CREATE TABLE sql_trace_test (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sql_trace_test(name) VALUES (?)`, "alpha"); err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}
	rows, err := db.Query(`SELECT id, name FROM sql_trace_test WHERE name = ?`, "alpha")
	if err != nil {
		t.Fatalf("SELECT failed: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err failed: %v", err)
	}

	createLog, ok := recorder.FindBySQL(`CREATE TABLE sql_trace_test (id INTEGER PRIMARY KEY, name TEXT)`)
	if !ok {
		t.Fatalf("CREATE TABLE log not found: %+v", recorder.Snapshot())
	}
	if got := createLog.attrs["kind"]; got != "exec" {
		t.Fatalf("CREATE TABLE kind mismatch: %q", got)
	}
	if got := createLog.attrs["operation"]; got != "CREATE" {
		t.Fatalf("CREATE TABLE operation mismatch: %q", got)
	}

	insertLog, ok := recorder.FindBySQL(`INSERT INTO sql_trace_test(name) VALUES (?)`)
	if !ok {
		t.Fatalf("INSERT log not found: %+v", recorder.Snapshot())
	}
	if got := insertLog.attrs["arg_count"]; got != "1" {
		t.Fatalf("INSERT arg_count mismatch: %q", got)
	}

	selectLog, ok := recorder.FindBySQL(`SELECT id, name FROM sql_trace_test WHERE name = ?`)
	if !ok {
		t.Fatalf("SELECT log not found: %+v", recorder.Snapshot())
	}
	if got := selectLog.attrs["kind"]; got != "query" {
		t.Fatalf("SELECT kind mismatch: %q", got)
	}
	if got := selectLog.attrs["operation"]; got != "SELECT" {
		t.Fatalf("SELECT operation mismatch: %q", got)
	}
	if got := selectLog.attrs["row_count"]; got != "1" {
		t.Fatalf("SELECT row_count mismatch: %q", got)
	}
}
