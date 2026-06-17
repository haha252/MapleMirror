package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"mirror-server/internal/protocol"
)

func TestDispatchSyncTasksBeforeAckConfirmsTaskAckBeforeOriginalAck(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-1', ?, 'inventory_reconcile', NULL, 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	control := ControlServer{Repo: repo}
	slots := 1
	heartbeat := envelopeWithPayload(t, session, protocol.TypeHeartbeat, 1, protocol.Heartbeat{
		Status: "syncing", SyncTaskSlotsAvailable: &slots,
	})
	result, err := control.handleMessage(session, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	done := make(chan error, 1)
	go func() {
		dispatched, err := control.writeResponsesAfterMessage(serverConn, session, "req-1", heartbeat, result)
		if err != nil {
			done <- err
			return
		}
		if dispatched != 1 {
			done <- fmt.Errorf("dispatch=%d want 1", dispatched)
			return
		}
		done <- nil
	}()
	readTaskFrame(t, clientConn)
	remainingSlots := 0
	taskAck := envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, 2, protocol.SyncTaskAck{
		TaskID: "task-1", State: "running", SyncTaskSlotsAvailable: &remainingSlots,
	})
	taskAck.MessageID = "task-1-ack"
	if err := protocol.WriteFrame(clientConn, taskAck); err != nil {
		t.Fatal(err)
	}
	ackForTaskAck := readFrame(t, clientConn)
	if ackForTaskAck.MessageType != protocol.TypeHeartbeatAck || ackForTaskAck.ReplyTo != taskAck.MessageID {
		t.Fatalf("task ack response type=%s reply_to=%s", ackForTaskAck.MessageType, ackForTaskAck.ReplyTo)
	}
	ackForHeartbeat := readFrame(t, clientConn)
	if ackForHeartbeat.MessageType != protocol.TypeHeartbeatAck || ackForHeartbeat.ReplyTo != heartbeat.MessageID {
		t.Fatalf("heartbeat response type=%s reply_to=%s", ackForHeartbeat.MessageType, ackForHeartbeat.ReplyTo)
	}
	var payload protocol.HeartbeatAckPayload
	if err := json.Unmarshal(ackForHeartbeat.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AcceptedSequence != 2 {
		t.Fatalf("heartbeat ack accepted_sequence=%d want 2", payload.AcceptedSequence)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInteractiveDispatchContinuesAfterTaskAckReportsSlots(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	for i := 1; i <= 2; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		mustExecControl(t, repo.DB, `INSERT INTO node_tasks
			(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
			VALUES (?, ?, 'inventory_reconcile', NULL, 'pending', 'req', 'now', 'now')`,
			taskID, session.NodeID)
	}
	control := ControlServer{Repo: repo}
	slots := 1
	heartbeat := envelopeWithPayload(t, session, protocol.TypeHeartbeat, 1, protocol.Heartbeat{
		Status: "syncing", SyncTaskSlotsAvailable: &slots,
	})
	result, err := control.handleMessage(session, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	done := make(chan error, 1)
	go func() {
		dispatched, err := control.writeResponsesAfterMessage(serverConn, session, "req-1", heartbeat, result)
		if err != nil {
			done <- err
			return
		}
		if dispatched != 2 {
			done <- fmt.Errorf("dispatch=%d want 2", dispatched)
			return
		}
		done <- nil
	}()
	for i := 1; i <= 2; i++ {
		task := readTaskFrame(t, clientConn)
		nextSlots := 1
		if i == 2 {
			nextSlots = 0
		}
		taskAck := envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, uint64(i+1), protocol.SyncTaskAck{
			TaskID: task.TaskID, State: "running", SyncTaskSlotsAvailable: &nextSlots,
		})
		taskAck.MessageID = task.TaskID + "-ack"
		if err := protocol.WriteFrame(clientConn, taskAck); err != nil {
			t.Fatal(err)
		}
		if ack := readFrame(t, clientConn); ack.ReplyTo != taskAck.MessageID {
			t.Fatalf("task ack %d reply_to=%s", i, ack.ReplyTo)
		}
	}
	if ack := readFrame(t, clientConn); ack.ReplyTo != heartbeat.MessageID {
		t.Fatalf("heartbeat reply_to=%s", ack.ReplyTo)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func readTaskFrame(t *testing.T, conn net.Conn) protocol.SyncTask {
	t.Helper()
	msg := readFrame(t, conn)
	if msg.MessageType != protocol.TypeSyncTask {
		t.Fatalf("frame=%s want %s", msg.MessageType, protocol.TypeSyncTask)
	}
	var task protocol.SyncTask
	if err := json.Unmarshal(msg.Payload, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func readFrame(t *testing.T, conn net.Conn) protocol.Envelope {
	t.Helper()
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}
