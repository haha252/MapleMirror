package control

import (
	"fmt"
	"net"
	"testing"

	"mirror-server/internal/protocol"
)

func TestReadControlFrameOrDispatchWakeSendsPendingTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 1)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-1', ?, 'inventory_reconcile', NULL, 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	repo.runtime().NotifySyncTasks(session.NodeID)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	control := ControlServer{Repo: repo, HeartbeatTimeout: 5}
	done := make(chan error, 1)
	go func() {
		_, dispatched, err := control.readControlFrameOrDispatchWake(serverConn, session, "req-1")
		if err != nil {
			done <- err
			return
		}
		if dispatched != 1 {
			done <- fmt.Errorf("dispatched=%d want 1", dispatched)
			return
		}
		done <- nil
	}()
	task := readTaskFrame(t, clientConn)
	remainingSlots := 0
	ack := envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, 1, protocol.SyncTaskAck{
		TaskID: task.TaskID, State: "running", SyncTaskSlotsAvailable: &remainingSlots,
	})
	ack.MessageID = "task-1-ack"
	if err := protocol.WriteFrame(clientConn, ack); err != nil {
		t.Fatal(err)
	}
	if got := readFrame(t, clientConn); got.ReplyTo != ack.MessageID {
		t.Fatalf("ack reply_to=%s", got.ReplyTo)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
		t.Fatal("wake should be consumed")
	}
}
