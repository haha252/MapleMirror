package mirrorsync

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestRetryTaskResetsRetryableBackoffStates(t *testing.T) {
	for _, state := range []string{"retry_wait", "failed"} {
		t.Run(state, func(t *testing.T) {
			db, store := retryStore(t)
			seedRetryTask(t, db, "task-1", state, "asset-1")

			if err := store.RetryTask(context.Background(), "node-1", "task-1"); err != nil {
				t.Fatal(err)
			}

			assertRetryReset(t, db, "task-1")
		})
	}
}

func TestRetryTaskRejectsNonRetryableStates(t *testing.T) {
	for _, state := range []string{"pending", "sent", "running", "succeeded", "cancelled", "obsolete"} {
		t.Run(state, func(t *testing.T) {
			db, store := retryStore(t)
			seedRetryTask(t, db, "task-1", state, "asset-1")

			err := store.RetryTask(context.Background(), "node-1", "task-1")
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("non-retryable state should be rejected, got %v", err)
			}
			assertRetryTaskState(t, db, "task-1", state)
		})
	}
}

func TestRetryTaskRejectsAssetlessTask(t *testing.T) {
	db, store := retryStore(t)
	seedRetryTask(t, db, "task-1", "retry_wait", "")

	err := store.RetryTask(context.Background(), "node-1", "task-1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("assetless task should be rejected, got %v", err)
	}
	assertRetryTaskState(t, db, "task-1", "retry_wait")
}

func TestRetryTaskRejectsInactiveTargets(t *testing.T) {
	cases := []struct {
		name   string
		mutate string
	}{
		{name: "target_remove", mutate: `UPDATE target_inventory SET desired_state = 'remove'`},
		{name: "project_disabled", mutate: `UPDATE projects SET enabled = 0 WHERE id = 'p1'`},
		{name: "release_unselected", mutate: `UPDATE releases SET selected = 0 WHERE id = 'rel-1'`},
		{name: "asset_removed", mutate: `UPDATE assets SET service_state = 'removed' WHERE id = 'asset-1'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, store := retryStore(t)
			seedRetryTask(t, db, "task-1", "retry_wait", "asset-1")
			mustExecRetry(t, db, tc.mutate)

			err := store.RetryTask(context.Background(), "node-1", "task-1")
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("inactive target should be rejected, got %v", err)
			}
			assertRetryTaskState(t, db, "task-1", "retry_wait")
		})
	}
}

func retryStore(t *testing.T) (*sql.DB, Store) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedNode(t, db)
	seedRetryAssetTarget(t, db)
	return db, Store{DB: db}
}

func seedRetryAssetTarget(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecRetry(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExecRetry(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	mustExecRetry(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'app.zip', 'amd64', 10,
		'https://example.invalid/app.zip', 'sha256:abc', 'candidate', 'now')`)
	mustExecRetry(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-1', 'required', 'now')`)
}

func seedRetryTask(t *testing.T, db *sql.DB, taskID, state, assetID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecRetry(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, attempts, retry_after, error_message)
		VALUES (?, 'node-1', 'asset_download', ?, ?, 'req-1', ?, ?, 3, ?, 'download failed')`,
		taskID, nullable(assetID), state, now, now,
		time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
}

func assertRetryReset(t *testing.T, db *sql.DB, taskID string) {
	t.Helper()
	var state string
	var attempts int
	var retryAfter sql.NullString
	var errMsg sql.NullString
	err := db.QueryRow(`SELECT state, attempts, retry_after, error_message
		FROM node_tasks WHERE id = ?`, taskID).Scan(&state, &attempts, &retryAfter, &errMsg)
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

func assertRetryTaskState(t *testing.T, db *sql.DB, taskID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT state FROM node_tasks WHERE id = ?`, taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("task state changed got=%q want=%q", got, want)
	}
}

func mustExecRetry(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}
