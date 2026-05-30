package mirrorsync

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestRetryTaskResetsBackoffState(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	seedNode(t, db)
	seedRetryTask(t, db)

	store := Store{DB: db}
	if err := store.RetryTask(context.Background(), "node-1", "task-1"); err != nil {
		t.Fatal(err)
	}

	var state string
	var attempts int
	var retryAfter sql.NullString
	var errMsg sql.NullString
	err = db.QueryRow(`SELECT state, attempts, retry_after, error_message
		FROM node_tasks WHERE id = 'task-1'`).Scan(&state, &attempts, &retryAfter, &errMsg)
	if err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempts != 0 {
		t.Fatalf("unexpected reset result state=%s attempts=%d", state, attempts)
	}
	if retryAfter.Valid && retryAfter.String != "" {
		t.Fatalf("retry_after should be cleared, got=%q", retryAfter.String)
	}
	if errMsg.Valid && errMsg.String != "" {
		t.Fatalf("error_message should be cleared, got=%q", errMsg.String)
	}
}

func seedRetryTask(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, attempts, retry_after, error_message)
		VALUES ('task-1', 'node-1', 'asset_download', NULL, 'retry_wait', 'req-1', ?, ?, 3, ?, 'download failed')`,
		now, now, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}
