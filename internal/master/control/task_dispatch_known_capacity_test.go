package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestDispatchSyncTasksAfterAckWithoutSlotsUsesKnownCapacity(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, lease_expires_at)
		VALUES ('task-sent', ?, 'asset_download', 'asset-1', 'sent', 'req', 'now', 'now', ?)`,
		session.NodeID, lease)
	for i := 1; i <= 2; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		mustExecControl(t, repo.DB, `INSERT INTO node_tasks
			(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
			VALUES (?, ?, 'inventory_reconcile', NULL, 'pending', 'req', 'now', 'now')`,
			taskID, session.NodeID)
	}
	control := ControlServer{Repo: repo}
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 2)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	initialDone := make(chan error, 1)
	go func() {
		dispatched, err := control.writeSyncTasks(serverConn, session, "req-1")
		if err != nil {
			initialDone <- err
			return
		}
		if dispatched != 1 {
			initialDone <- fmt.Errorf("initial dispatch=%d want 1", dispatched)
			return
		}
		initialDone <- nil
	}()
	msg, err := protocol.ReadFrame(clientConn, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	var task protocol.SyncTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "task-1" {
		t.Fatalf("first task=%s want task-1", task.TaskID)
	}
	if err := <-initialDone; err != nil {
		t.Fatal(err)
	}

	ack := envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, 2, protocol.SyncTaskAck{
		TaskID: "task-1", State: "running",
	})
	ackResult, err := control.handleMessage(session, ack)
	if err != nil {
		t.Fatal(err)
	}
	if ackResult.SyncTaskSlotsKnown {
		t.Fatalf("ack without slots should not refresh capacity: %+v", ackResult)
	}
	dispatchDone := make(chan error, 1)
	go func() {
		dispatched, err := control.dispatchSyncTasksAfterMessage(serverConn, session, "req-2", ackResult)
		if err != nil {
			dispatchDone <- err
			return
		}
		if dispatched != 1 {
			dispatchDone <- fmt.Errorf("post-ack dispatch=%d want 1", dispatched)
			return
		}
		dispatchDone <- nil
	}()
	msg, err = protocol.ReadFrame(clientConn, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "task-2" {
		t.Fatalf("second task=%s want task-2", task.TaskID)
	}
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}
}
