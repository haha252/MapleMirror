package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

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
