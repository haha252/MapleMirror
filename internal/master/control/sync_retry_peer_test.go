package control

import (
	"context"
	"database/sql"
	"testing"

	"mirror-server/internal/protocol"
)

func TestTemporaryErrorIgnoresPublicProbeBlockedPeer(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo.PublicProbeNetworkFailures = 5

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
	seedPeerNode(t, repo, "node-2", "源节点", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	markPeerPublicProbeFailures(t, repo, "node-2", 5)
	markTaskRunning(t, repo, "task-1")

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
	if err != nil || state != "retry_wait" || !retryAfter.Valid || retryAfter.String == "" {
		t.Fatalf("public-probe blocked peer should not force immediate retry, state=%s retry=%q err=%v",
			state, retryAfter.String, err)
	}
}
