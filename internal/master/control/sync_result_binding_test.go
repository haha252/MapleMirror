package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestDownloadResultRejectsEmptyAssetID(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-empty", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-empty")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-empty",
		Result:            "succeeded",
		LocalDigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SizeBytes:         10,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTaskRetriedWithoutInventory(t, repo, "task-empty")
}

func TestDownloadResultRejectsMismatchedAssetID(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-mismatch", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-mismatch")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-mismatch",
		AssetID:           "asset-other",
		Result:            "succeeded",
		LocalDigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SizeBytes:         10,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTaskRetriedWithoutInventory(t, repo, "task-mismatch")
}

func assertTaskRetriedWithoutInventory(t *testing.T, repo Repository, taskID string) {
	t.Helper()
	var state, message string
	if err := repo.DB.QueryRow(`SELECT state, COALESCE(error_message, '')
		FROM node_tasks WHERE id = ?`, taskID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "retry_wait" || message != "同步结果资产与任务不匹配" {
		t.Fatalf("task state=%s message=%q", state, message)
	}
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1'", 0)
}
