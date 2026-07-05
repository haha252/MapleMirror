package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestCheckpointWALRecoversFromLockedPooledConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	settings := walSettings{AutocheckpointPages: 0, JournalSizeLimitBytes: 0, TruncateThresholdBytes: 0}
	if err := configure(db, 5*time.Second, true, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items DEFAULT VALUES`); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var leakedRows driver.Rows
	if err := conn.Raw(func(raw any) error {
		queryer, ok := raw.(driver.QueryerContext)
		if !ok {
			return fmt.Errorf("driver does not implement QueryerContext")
		}
		leakedRows, err = queryer.QueryContext(context.Background(), `SELECT id FROM items`, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if leakedRows == nil {
		t.Fatal("expected unfinished driver rows")
	}

	probe, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var busy, logFrames, checkedFrames int
	err = probe.QueryRowContext(context.Background(), `PRAGMA wal_checkpoint(PASSIVE)`).
		Scan(&busy, &logFrames, &checkedFrames)
	if closeErr := probe.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !isSQLiteLocked(err) {
		t.Fatalf("test setup did not produce SQLITE_LOCKED: %v", err)
	}

	if err := CheckpointWAL(db, path, settings.TruncateThresholdBytes, nil); err != nil {
		t.Fatalf("checkpoint did not recover from SQLITE_LOCKED: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("replacement connection is unusable: %v", err)
	}
}
