package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestStartSessionResetsLeasedRunningTaskForRedispatch(t *testing.T) {
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
	var state, leaseAfter string
	err = repo.DB.QueryRow(`SELECT state, COALESCE(lease_expires_at, '') FROM node_tasks
		WHERE id = 'task-running'`).Scan(&state, &leaseAfter)
	if err != nil || state != "pending" || leaseAfter != "" {
		t.Fatalf("重连后运行任务应重派 state=%q lease=%q err=%v", state, leaseAfter, err)
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

func TestRunningAckDoesNotReviveRetryWaitTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	retryAfter := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	seedDownloadTask(t, repo, session.NodeID, "task-retry", "asset-1", 1, retryAfter)

	_, err := repo.AcceptSyncTaskAck(context.Background(), session, 1, protocol.SyncTaskAck{
		TaskID: "task-retry", State: "running", Message: "任务仍在执行",
	})
	if err == nil || !strings.Contains(err.Error(), "ACK 无效") {
		t.Fatalf("retry_wait 任务不应接受 stale running ACK，err=%v", err)
	}
	var state string
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks
		WHERE id = 'task-retry'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "retry_wait" {
		t.Fatalf("stale running ACK 不得回退任务状态 state=%s", state)
	}
	last, err := repo.currentSequence(session)
	if err != nil {
		t.Fatal(err)
	}
	if last != 0 {
		t.Fatalf("无效 running ACK 不应推进序号 got=%d", last)
	}
}
