package control

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestNextSyncTaskDispatchesRetryWaitAfterDeadline(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	retryAfter := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 1, retryAfter)

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || task.TaskID != "task-1" {
		t.Fatalf("expected retry_wait task to dispatch, ok=%v task=%+v", ok, task)
	}
}

func TestStartSessionResetsInterruptedSentTaskForRedispatch(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-sent', ?, 'asset_download', 'asset-1', 'sent', 'req-1', 'old', 'old')`,
		session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := repo.StartSession(context.Background(), "sha256:aa", "req-reconnect")
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.NextSyncTask(context.Background(), restarted.NodeID)
	if err != nil || !ok || task.TaskID != "task-sent" {
		t.Fatalf("interrupted sent task should redispatch, ok=%v task=%+v err=%v", ok, task, err)
	}
}

func TestNextSyncTaskHandlesAssetlessTaskWithoutBlockingQueue(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-reconcile', ?, 'inventory_reconcile', NULL, 'pending',
		'req-1', 'old', 'old')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || task.TaskID != "task-reconcile" || task.Asset.AssetID != "" {
		t.Fatalf("assetless task should dispatch safely, ok=%v task=%+v err=%v", ok, task, err)
	}
}

func seedDownloadTask(t *testing.T, repo Repository, nodeID, taskID, assetID string, attempts int, retryAfter string) {
	t.Helper()
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, attempts, retry_after)
		VALUES (?, ?, 'asset_download', ?, 'retry_wait', ?, ?, ?, ?, ?)`,
		taskID, nodeID, assetID, taskID, "now", "now", attempts, nullable(retryAfter))
	if err != nil {
		t.Fatal(err)
	}
}

func assertTaskRetryState(t *testing.T, repo Repository, taskID, wantState string, wantAttempts int, start time.Time, minWait, maxWait time.Duration) {
	t.Helper()
	var gotState string
	var gotAttempts int
	var retryAfter sql.NullString
	err := repo.DB.QueryRow(`SELECT state, attempts, retry_after FROM node_tasks WHERE id = ?`, taskID).
		Scan(&gotState, &gotAttempts, &retryAfter)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != wantState || gotAttempts != wantAttempts {
		t.Fatalf("unexpected retry state state=%s attempts=%d", gotState, gotAttempts)
	}
	if wantState == "failed" {
		if retryAfter.Valid && retryAfter.String != "" {
			t.Fatalf("failed task should clear retry_after, got=%q", retryAfter.String)
		}
		return
	}
	if !retryAfter.Valid || retryAfter.String == "" {
		t.Fatal("retry_wait task should have retry_after")
	}
	retryAt, err := time.Parse(time.RFC3339Nano, retryAfter.String)
	if err != nil {
		t.Fatal(err)
	}
	wait := retryAt.Sub(start)
	if wait < minWait || wait > maxWait {
		t.Fatalf("unexpected retry delay %s, want between %s and %s", wait, minWait, maxWait)
	}
}
