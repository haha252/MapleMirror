package control

import (
	"context"
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
