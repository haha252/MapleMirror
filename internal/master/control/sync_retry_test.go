package control

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestAcceptSyncTaskResultAppliesRetryBackoff(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	expected := []struct {
		sequence uint64
		state    string
		attempts int
		minWait  time.Duration
		maxWait  time.Duration
	}{
		{sequence: 1, state: "retry_wait", attempts: 1, minWait: 4 * time.Second, maxWait: 6 * time.Second},
		{sequence: 2, state: "retry_wait", attempts: 2, minWait: 9 * time.Second, maxWait: 11 * time.Second},
		{sequence: 3, state: "retry_wait", attempts: 3, minWait: 29 * time.Second, maxWait: 31 * time.Second},
		{sequence: 4, state: "retry_wait", attempts: 4, minWait: 59 * time.Second, maxWait: 61 * time.Second},
		{sequence: 5, state: "retry_wait", attempts: 5, minWait: 179 * time.Second, maxWait: 181 * time.Second},
		{sequence: 6, state: "failed", attempts: 6},
	}

	for _, item := range expected {
		start := time.Now().UTC()
		_, err := repo.AcceptSyncTaskResult(context.Background(), session, item.sequence, protocol.SyncTaskResult{
			TaskID:  "task-1",
			AssetID: "asset-1",
			Result:  "temporary_error",
			Message: "download failed",
		})
		if err != nil {
			t.Fatal(err)
		}
		assertTaskRetryState(t, repo, "task-1", item.state, item.attempts, start, item.minWait, item.maxWait)
	}
}

func TestNextSyncTaskSkipsRetryWaitBeforeDeadline(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	retryAfter := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 1, retryAfter)

	_, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("retry_wait task should not dispatch before retry_after")
	}
}

func TestDigestMismatchTaskResultRetriesWithoutQuarantine(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	start := time.Now().UTC()
	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-1",
		AssetID:           "asset-1",
		Result:            "digest_mismatch",
		LocalDigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SizeBytes:         10,
		Message:           "资产摘要不匹配",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskRetryState(t, repo, "task-1", "retry_wait", 1, start, 4*time.Second, 6*time.Second)
	var state, certStatus string
	_ = repo.DB.QueryRow("SELECT state FROM nodes WHERE id = ?", session.NodeID).Scan(&state)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	if state == "disabled" || certStatus != "active" {
		t.Fatalf("下载阶段摘要不一致不应隔离节点 state=%s cert=%s", state, certStatus)
	}
}

func TestTemporaryErrorWithVerifiedPeerRetriesImmediately(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
	seedPeerNode(t, repo, "node-2", "源节点", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:  "task-1",
		AssetID: "asset-1",
		Result:  "temporary_error",
		Message: "源站不可用",
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var retryAfter sql.NullString
	err = repo.DB.QueryRow(`SELECT state, retry_after FROM node_tasks WHERE id = 'task-1'`).
		Scan(&state, &retryAfter)
	if err != nil || state != "pending" || (retryAfter.Valid && retryAfter.String != "") {
		t.Fatalf("verified peer should retry immediately, state=%s retry=%q err=%v",
			state, retryAfter.String, err)
	}
}

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
