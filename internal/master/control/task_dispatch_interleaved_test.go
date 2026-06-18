package control

import (
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestInteractiveDispatchAcceptsInterleavedPressureReport(t *testing.T) {
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
	task := readTaskFrame(t, clientConn)
	zero := 0
	pressure := envelopeWithPayload(t, session, protocol.TypePressureReport, 2, protocol.PressureReport{
		ReportID: "pressure-1", SampledAt: time.Now().UTC(), SampleWindowSeconds: 10,
		SyncTaskSlotsAvailable: &zero,
	})
	pressure.MessageID = "pressure-1"
	if err := protocol.WriteFrame(clientConn, pressure); err != nil {
		t.Fatal(err)
	}
	if ack := readFrame(t, clientConn); ack.ReplyTo != pressure.MessageID {
		t.Fatalf("pressure ack reply_to=%s", ack.ReplyTo)
	}
	taskAck := envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, 3, protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running", SyncTaskSlotsAvailable: &zero,
	})
	taskAck.MessageID = task.TaskID + "-ack"
	if err := protocol.WriteFrame(clientConn, taskAck); err != nil {
		t.Fatal(err)
	}
	if ack := readFrame(t, clientConn); ack.ReplyTo != taskAck.MessageID {
		t.Fatalf("task ack reply_to=%s", ack.ReplyTo)
	}
	if ack := readFrame(t, clientConn); ack.ReplyTo != heartbeat.MessageID {
		t.Fatalf("heartbeat ack reply_to=%s", ack.ReplyTo)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
