package control

import (
	"context"
	"strings"
	"testing"

	"mirror-server/internal/protocol"
)

func TestAcceptSyncTaskAckRejectsNeverSentPendingTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, updated_at)
		VALUES ('task-pending', ?, 'inventory_reconcile', 'pending', 'req', 'now', 'now')`, session.NodeID)

	_, err := repo.AcceptSyncTaskAck(context.Background(), session, 1, protocol.SyncTaskAck{
		TaskID: "task-pending", State: "running",
	})
	if err == nil || !strings.Contains(err.Error(), "ACK 无效") {
		t.Fatalf("pending task ack should fail, err=%v", err)
	}
	last, err := repo.currentSequence(session)
	if err != nil {
		t.Fatal(err)
	}
	if last != 0 {
		t.Fatalf("pending task ack must not advance sequence, got %d", last)
	}
	var state string
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id = 'task-pending'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("pending task state changed to %s", state)
	}
}
