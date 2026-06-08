package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestAssetDeleteResultMarksInventoryRemoved(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `UPDATE target_inventory SET desired_state = 'remove'
		WHERE node_id = ? AND asset_id = 'asset-1'`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		10, 'now', 'verified')`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('delete-1', ?, 'asset_delete', 'asset-1', 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	markTaskRunning(t, repo, "delete-1")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:  "delete-1",
		AssetID: "asset-1",
		Result:  "succeeded",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSyncTaskInventory(t, repo, "delete-1", "succeeded", "removed")
}
