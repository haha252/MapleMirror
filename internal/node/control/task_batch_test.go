package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

type recordingExecutor struct {
	tasks chan string
}

func (e recordingExecutor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	select {
	case e.tasks <- task.TaskID:
	case <-ctx.Done():
	}
	return protocol.SyncTaskResult{
		TaskID: task.TaskID, AssetID: task.Asset.AssetID, Result: "succeeded",
	}
}

func TestReadOptionalTasksRefillsBeyondTenWhenCapacityAvailable(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	executed := make(chan string, 12)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 12; i++ {
			taskID := fmt.Sprintf("task-%02d", i)
			writeSyncTask(t, server, taskID)
			ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
			if !ok {
				return
			}
			wantSeq := uint64(2 + i)
			if ack.Sequence != wantSeq {
				t.Errorf("%s ack sequence = %d, want %d", taskID, ack.Sequence, wantSeq)
				return
			}
			sendAck(server, ack)
		}
	}()
	ctl := Client{
		NodeID:      "node-1",
		Executor:    recordingExecutor{tasks: executed},
		TaskLimiter: NewTaskLimiter(12),
	}
	next, err := ctl.readOptionalTasksToCapacity(client, "req-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if next != 15 {
		t.Fatalf("next sequence = %d, want 15", next)
	}
	<-done
	seen := map[string]bool{}
	for len(seen) < 12 {
		select {
		case id := <-executed:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("tasks not executed: %+v", seen)
		}
	}
}

func TestReadOptionalTasksAcksBatchInSingleSession(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	executed := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeSyncTask(t, server, "task-1")
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok || ack.Sequence != 3 {
			t.Errorf("first ack sequence = %d", ack.Sequence)
			return
		}
		sendAck(server, ack)
		writeSyncTask(t, server, "task-2")
		ack, ok = expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok || ack.Sequence != 4 {
			t.Errorf("second ack sequence = %d", ack.Sequence)
			return
		}
		sendAck(server, ack)
	}()
	ctl := Client{NodeID: "node-1", Executor: recordingExecutor{tasks: executed}}
	next, err := ctl.readOptionalTasks(client, "req-1", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if next != 5 {
		t.Fatalf("next sequence = %d, want 5", next)
	}
	<-done
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case id := <-executed:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("tasks not executed: %+v", seen)
		}
	}
}

func TestReadOptionalTasksReturnsProtocolError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		body, _ := json.Marshal(protocol.ProtocolError{
			Code:    "CONTROL_MESSAGE_ERROR",
			Message: "bad task",
		})
		_ = protocol.WriteFrame(server, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "err",
			MessageType:     protocol.TypeProtocolError,
			SentAt:          time.Now().UTC(),
			NodeID:          "node-1",
			RequestID:       "req-1",
			Payload:         body,
		})
	}()
	ctl := Client{NodeID: "node-1"}
	if _, err := ctl.readOptionalTasks(client, "req-1", 3, 1); err == nil {
		t.Fatal("expected optional task protocol error to propagate")
	}
	<-done
}

func TestRunOnceReadsTaskDispatchedAfterPressureReport(t *testing.T) {
	executed := make(chan string, 1)
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		hello, ok := expectType(t, conn, protocol.TypeHello)
		if !ok {
			return
		}
		sendWelcome(conn, hello)
		hb, ok := expectType(t, conn, protocol.TypeHeartbeat)
		if !ok {
			return
		}
		sendAck(conn, hb)
		pressure, ok := expectType(t, conn, protocol.TypePressureReport)
		if !ok {
			return
		}
		sendAck(conn, pressure)
		writeSyncTaskFor(t, conn, pressure, "task-after-pressure")
		ack, ok := expectType(t, conn, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		if ack.Sequence != 4 {
			t.Errorf("sync task ack sequence = %d, want 4", ack.Sequence)
			return
		}
		sendAck(conn, ack)
	})
	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		Executor:       recordingExecutor{tasks: executed},
		TaskLimiter:    NewTaskLimiter(1),
	}
	if _, err := client.RunOnce(); !runOnceEndedByPeer(err) {
		t.Fatal(err)
	}
	select {
	case id := <-executed:
		if id != "task-after-pressure" {
			t.Fatalf("executed task = %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("task dispatched after pressure report was not executed")
	}
}

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

func TestPendingTaskResultNotMarkedReportedWithoutAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		_, _ = protocol.ReadFrame(server, protocol.MaxFrameBytes)
	}()
	ctl := Client{NodeID: "node-1", DB: db}
	if _, err := ctl.sendPendingTaskResults(client, "req-1", 3); err == nil {
		t.Fatal("expected missing ACK error")
	}
	<-done
	var reportedAt string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '')
		FROM pending_sync_task_results WHERE task_id = 'task-1'`).Scan(&reportedAt); err != nil {
		t.Fatal(err)
	}
	if reportedAt != "" {
		t.Fatalf("reported_at should stay empty without ACK, got %q", reportedAt)
	}
}

func TestPendingTaskResultNotMarkedReportedWithWrongReplyAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, server, protocol.TypeSyncTaskResult)
		if !ok {
			return
		}
		body, _ := json.Marshal(protocol.HeartbeatAckPayload{
			AcceptedSequence: msg.Sequence,
		})
		_ = protocol.WriteFrame(server, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "wrong-ack",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          msg.NodeID,
			RequestID:       msg.RequestID,
			ReplyTo:         "other-message",
			Payload:         body,
		})
	}()
	ctl := Client{NodeID: "node-1", DB: db}
	if _, err := ctl.sendPendingTaskResults(client, "req-1", 3); err == nil {
		t.Fatal("expected wrong reply ack error")
	}
	<-done
	var reportedAt string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '')
		FROM pending_sync_task_results WHERE task_id = 'task-1'`).Scan(&reportedAt); err != nil {
		t.Fatal(err)
	}
	if reportedAt != "" {
		t.Fatalf("reported_at should stay empty after wrong ack, got %q", reportedAt)
	}
}

func TestPendingTaskResultNotMarkedReportedWithStaleSequenceAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, server, protocol.TypeSyncTaskResult)
		if !ok {
			return
		}
		body, _ := json.Marshal(protocol.HeartbeatAckPayload{
			AcceptedSequence: msg.Sequence - 1,
		})
		_ = protocol.WriteFrame(server, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "stale-ack",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          msg.NodeID,
			RequestID:       msg.RequestID,
			ReplyTo:         msg.MessageID,
			Payload:         body,
		})
	}()
	ctl := Client{NodeID: "node-1", DB: db}
	if _, err := ctl.sendPendingTaskResults(client, "req-1", 3); err == nil {
		t.Fatal("expected stale sequence ack error")
	}
	<-done
	var reportedAt string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '')
		FROM pending_sync_task_results WHERE task_id = 'task-1'`).Scan(&reportedAt); err != nil {
		t.Fatal(err)
	}
	if reportedAt != "" {
		t.Fatalf("reported_at should stay empty after stale ack, got %q", reportedAt)
	}
}

func writeSyncTask(t *testing.T, conn net.Conn, taskID string) {
	t.Helper()
	writeSyncTaskFor(t, conn, protocol.Envelope{
		NodeID:    "node-1",
		RequestID: "req-1",
	}, taskID)
}

func writeSyncTaskFor(t *testing.T, conn net.Conn, msg protocol.Envelope, taskID string) {
	t.Helper()
	body, _ := json.Marshal(protocol.SyncTask{
		TaskID: taskID, TaskType: "asset_delete",
		Asset: protocol.SyncAsset{AssetID: "asset-" + taskID},
	})
	if err := protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       taskID,
		MessageType:     protocol.TypeSyncTask,
		SentAt:          time.Now().UTC(),
		NodeID:          msg.NodeID,
		RequestID:       msg.RequestID,
		Payload:         body,
	}); err != nil {
		t.Error(err)
	}
}
