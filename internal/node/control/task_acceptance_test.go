package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestHandleDispatchedTaskRecordsRunningBeforeAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		var state string
		if err := db.QueryRow(`SELECT state FROM local_sync_tasks
			WHERE task_id = 'task-1'`).Scan(&state); err != nil {
			t.Error(err)
			return
		}
		if state != "running" {
			t.Errorf("local task state before ack response = %s", state)
			return
		}
		sendAck(server, ack)
	}()
	body, _ := json.Marshal(protocol.SyncTask{
		TaskID: "task-1", TaskType: "asset_download",
		Asset: protocol.SyncAsset{AssetID: "asset-1"},
	})
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}
	next, err := ctl.handleDispatchedTask(client, "req-1", 3, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "task-1",
		MessageType:     protocol.TypeSyncTask,
		SentAt:          time.Now().UTC(),
		NodeID:          "node-1",
		RequestID:       "req-1",
		Payload:         body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if next != 4 {
		t.Fatalf("next sequence=%d want 4", next)
	}
	<-done
}

func TestHandleDispatchedTaskRevertsRunningTaskWhenAckFails(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		_, _ = protocol.ReadFrame(server, protocol.MaxFrameBytes)
	}()
	body, _ := json.Marshal(protocol.SyncTask{
		TaskID: "task-1", TaskType: "asset_download",
		Asset: protocol.SyncAsset{AssetID: "asset-1"},
	})
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}
	if _, err := ctl.handleDispatchedTask(client, "req-1", 3, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "task-1",
		MessageType:     protocol.TypeSyncTask,
		SentAt:          time.Now().UTC(),
		NodeID:          "node-1",
		RequestID:       "req-1",
		Payload:         body,
	}); err == nil {
		t.Fatal("expected ack failure")
	}
	<-done
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_sync_tasks WHERE task_id = 'task-1'`).
		Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed ack must revert running task, count=%d", count)
	}
	if ctl.TaskLimiter.InFlight() != 0 {
		t.Fatalf("failed ack should release reserved slot, in_flight=%d", ctl.TaskLimiter.InFlight())
	}
}
