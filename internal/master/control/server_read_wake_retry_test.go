package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestWakeRemainsPendingWhenNodeHasNoSyncTaskSlots(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 0)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-1', ?, 'inventory_reconcile', NULL, 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	repo.runtime().NotifySyncTasks(session.NodeID)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	control := ControlServer{Repo: repo, HeartbeatTimeout: time.Second}

	done := make(chan int, 1)
	go func() {
		_, dispatched, err := control.readControlFrameOrDispatchWake(serverConn, session, "req-1")
		if err == nil {
			done <- dispatched
			return
		}
		done <- -1
	}()
	time.Sleep(20 * time.Millisecond)
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 1)

	task := readTaskFrame(t, clientConn)
	if task.TaskID != "task-1" {
		t.Fatalf("task id=%s want task-1", task.TaskID)
	}
	ackBody, _ := json.Marshal(protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running", SyncTaskSlotsAvailable: ptrInt(0),
	})
	if err := protocol.WriteFrame(clientConn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       task.TaskID + "-ack",
		MessageType:     protocol.TypeSyncTaskAck,
		SentAt:          time.Now().UTC(),
		NodeID:          session.NodeID,
		RequestID:       "req-1",
		Sequence:        2,
		ReplyTo:         task.TaskID,
		Payload:         ackBody,
	}); err != nil {
		t.Fatal(err)
	}
	if ack := readFrame(t, clientConn); ack.ReplyTo != task.TaskID+"-ack" {
		t.Fatalf("ack reply_to=%s", ack.ReplyTo)
	}
	if dispatched := <-done; dispatched != 1 {
		t.Fatalf("dispatched=%d want 1", dispatched)
	}
}

func ptrInt(v int) *int { return &v }
