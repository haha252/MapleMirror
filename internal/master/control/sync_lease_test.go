package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestStartSessionKeepsLeasedRunningTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	lease := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-running', ?, 'asset_download', 'asset-1', 'running',
		'req-1', 'old', 'old', ?)`, session.NodeID, lease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.StartSession(context.Background(), "sha256:aa", "req-reconnect"); err != nil {
		t.Fatal(err)
	}
	var state string
	err = repo.DB.QueryRow(`SELECT state FROM node_tasks
		WHERE id = 'task-running'`).Scan(&state)
	if err != nil || state != "running" {
		t.Fatalf("未过期运行任务不应重派 state=%q err=%v", state, err)
	}
}

func TestSyncTaskAckRenewsRunningLease(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at,
		updated_at, lease_expires_at)
		VALUES ('task-running', ?, 'asset_download', 'asset-1', 'running',
		'req-1', 'old', 'old', 'old')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	_, err = repo.AcceptSyncTaskAck(context.Background(), session, 1, protocol.SyncTaskAck{
		TaskID: "task-running", State: "running", Message: "任务仍在执行",
	})
	if err != nil {
		t.Fatal(err)
	}
	var lease string
	if err := repo.DB.QueryRow(`SELECT lease_expires_at FROM node_tasks
		WHERE id = 'task-running'`).Scan(&lease); err != nil {
		t.Fatal(err)
	}
	leaseAt, err := time.Parse(time.RFC3339Nano, lease)
	if err != nil || !leaseAt.After(before) {
		t.Fatalf("running ack should renew lease lease=%q err=%v", lease, err)
	}
}
