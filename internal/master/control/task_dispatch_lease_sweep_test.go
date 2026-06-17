package control

import (
	"testing"
	"time"
)

func TestWriteSyncTasksReclaimsExpiredLeaseEvenWhenReportedSlotsZero(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-expired', ?, 'asset_download', 'asset-1', 'running',
		'req', 'now', 'now', ?)`, session.NodeID, expired)
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 0)

	dispatched, err := (ControlServer{Repo: repo}).writeSyncTasks(nil, session, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if dispatched != 0 {
		t.Fatalf("capacity zero should not dispatch, got %d", dispatched)
	}
	var state, lease string
	if err := repo.DB.QueryRow(`SELECT state, COALESCE(lease_expires_at, '')
		FROM node_tasks WHERE id = 'task-expired'`).Scan(&state, &lease); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || lease != "" {
		t.Fatalf("expired lease should be reclaimed, state=%s lease=%q", state, lease)
	}
}
