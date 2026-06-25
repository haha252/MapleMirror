package control

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestStaleSyncTaskResultDoesNotMutateTaskOrInventory(t *testing.T) {
	cases := []struct {
		name       string
		state      string
		attempts   int
		retryAfter string
	}{
		{name: "retry_wait", state: "retry_wait", attempts: 2,
			retryAfter: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)},
		{name: "failed", state: "failed", attempts: 6},
		{name: "cancelled", state: "cancelled", attempts: 1},
		{name: "succeeded", state: "succeeded", attempts: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()

			session := seedNodeAndSession(t, repo)
			seedAssetTarget(t, repo, session.NodeID)
			seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET state = ?,
				attempts = ?, retry_after = ?, lease_expires_at = NULL
				WHERE id = 'task-1'`, tc.state, tc.attempts, nullable(tc.retryAfter))

			result, err := repo.AcceptSyncTaskResult(context.Background(), session, 1,
				successfulTaskResult("task-1", "asset-1"))
			if err != nil {
				t.Fatal(err)
			}
			if result.AcceptedSequence != 1 {
				t.Fatalf("stale result should still ACK sequence, got=%d", result.AcceptedSequence)
			}
			assertTaskUnchanged(t, repo, "task-1", tc.state, tc.attempts, tc.retryAfter)
			assertTableCount(t, repo, "node_inventory",
				"node_id = 'node-1' AND asset_id = 'asset-1'", 0)
		})
	}
}

func TestExpiredLeaseSyncTaskResultDoesNotPublishVerifiedAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedPendingLatestTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-new", "asset-new", 0, "")
	expired := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state = 'running',
		lease_expires_at = ? WHERE id = 'task-new'`, expired)

	result, err := repo.AcceptSyncTaskResult(context.Background(), session, 1,
		successfulTaskResult("task-new", "asset-new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.AcceptedSequence != 1 {
		t.Fatalf("expired result should still ACK sequence, got=%d", result.AcceptedSequence)
	}
	assertTaskUnchanged(t, repo, "task-new", "running", 0, "")
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new'", 0)
	assertControlAssetState(t, repo, "asset-new", "pending")
}

func TestUnknownSyncTaskResultIsAcknowledged(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)

	result, err := repo.AcceptSyncTaskResult(context.Background(), session, 1,
		successfulTaskResult("missing-task", "asset-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.AcceptedSequence != 1 {
		t.Fatalf("unknown result should still ACK sequence, got=%d", result.AcceptedSequence)
	}
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND id = 'missing-task'", 0)
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1'", 0)
}

func TestLeasedSentSyncTaskResultCanComplete(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state = 'sent',
		lease_expires_at = ? WHERE id = 'task-1'`, lease)

	result, err := repo.AcceptSyncTaskResult(context.Background(), session, 1,
		successfulTaskResult("task-1", "asset-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.AcceptedSequence != 1 {
		t.Fatalf("accepted sequence = %d, want 1", result.AcceptedSequence)
	}
	assertTaskUnchanged(t, repo, "task-1", "succeeded", 0, "")
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND state = 'verified'", 1)
}

func successfulTaskResult(taskID, assetID string) protocol.SyncTaskResult {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	size := int64(10)
	if assetID == "asset-new" {
		digest = "sha256:new"
		size = 20
	}
	return protocol.SyncTaskResult{
		TaskID:            taskID,
		AssetID:           assetID,
		Result:            "succeeded",
		LocalDigestSHA256: digest,
		SizeBytes:         size,
	}
}

func assertTaskUnchanged(t *testing.T, repo Repository, taskID, wantState string,
	wantAttempts int, wantRetryAfter string) {
	t.Helper()
	var gotState string
	var gotAttempts int
	var gotRetryAfter sql.NullString
	err := repo.DB.QueryRow(`SELECT state, attempts, retry_after FROM node_tasks
		WHERE id = ?`, taskID).Scan(&gotState, &gotAttempts, &gotRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != wantState || gotAttempts != wantAttempts {
		t.Fatalf("task changed state=%s attempts=%d", gotState, gotAttempts)
	}
	if gotRetryAfter.String != wantRetryAfter {
		t.Fatalf("retry_after changed got=%q want=%q", gotRetryAfter.String, wantRetryAfter)
	}
}
