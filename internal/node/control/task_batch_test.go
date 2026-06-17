package control

import (
	"context"
	"encoding/json"
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
	if _, err := ctl.sendPendingTaskResults(client, "req-1", 3, nil); err == nil {
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

func writeSyncTask(t *testing.T, conn net.Conn, taskID string) {
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
		NodeID:          "node-1",
		RequestID:       "req-1",
		Payload:         body,
	}); err != nil {
		t.Error(err)
	}
}
