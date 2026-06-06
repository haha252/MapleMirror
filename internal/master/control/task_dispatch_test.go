package control

import (
	"context"
	"strings"
	"testing"

	"mirror-server/internal/protocol"
)

func TestShouldDispatchNextTaskOnlyAfterInventoryOrTaskResult(t *testing.T) {
	for _, messageType := range []string{
		protocol.TypeHeartbeat,
		protocol.TypeTrafficEvent,
		protocol.TypePressureReport,
		protocol.TypeSyncTaskAck,
	} {
		if shouldDispatchNextTask(messageType) {
			t.Fatalf("%s should not trigger sync task dispatch", messageType)
		}
	}
	for _, messageType := range []string{
		protocol.TypeInventoryReport,
		protocol.TypeSyncTaskResult,
	} {
		if !shouldDispatchNextTask(messageType) {
			t.Fatalf("%s should trigger sync task dispatch", messageType)
		}
	}
}

func TestAcceptSyncTaskAckMarksTaskRunning(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, updated_at)
		VALUES ('task-ack', ?, 'inventory_reconcile', 'sent', 'req', 'now', 'now')`, session.NodeID)

	result, err := repo.AcceptSyncTaskAck(context.Background(), session, 1, protocol.SyncTaskAck{
		TaskID: "task-ack", State: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id = 'task-ack'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" || result.AcceptedSequence != 1 {
		t.Fatalf("同步任务 ACK 应进入 running state=%s result=%+v", state, result)
	}
}

func TestAcceptSyncTaskAckRejectsUnknownTaskWithoutAdvancingSequence(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, updated_at)
		VALUES ('task-real', ?, 'inventory_reconcile', 'sent', 'req', 'now', 'now')`, session.NodeID)

	_, err := repo.AcceptSyncTaskAck(context.Background(), session, 9, protocol.SyncTaskAck{
		TaskID: "task-missing", State: "running",
	})
	if err == nil || !strings.Contains(err.Error(), "ACK 无效") {
		t.Fatalf("unknown task ack should fail, err=%v", err)
	}
	last, err := repo.currentSequence(session)
	if err != nil {
		t.Fatal(err)
	}
	if last != 0 {
		t.Fatalf("invalid ack must not advance sequence, got %d", last)
	}
	var state string
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id = 'task-real'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "sent" {
		t.Fatalf("unrelated task state changed to %s", state)
	}
}
