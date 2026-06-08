package control

import (
	"context"
	"testing"
)

func TestNextSyncTaskSkipsInvalidAssetDownloadTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-stale', ?, 'asset_download', 'asset-1', 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	mustExecControl(t, repo.DB, `UPDATE projects SET enabled = 0 WHERE id = 'p1'`)
	mustExecControl(t, repo.DB, `UPDATE target_inventory SET desired_state = 'remove'
		WHERE node_id = ? AND asset_id = 'asset-1'`, session.NodeID)

	_, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("invalid asset download task should not dispatch")
	}
}
