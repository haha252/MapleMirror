package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestSendTrafficEventHandlesInterleavedSyncTask(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO pending_traffic_events
		(event_sequence, authorization_id, node_request_id, master_request_id,
		sent_bytes, created_at, asset_id, status)
		VALUES (1, 'auth-1', 'node-req-1', 'master-req-1', 12, ?, 'asset-1', 'completed')`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := protocol.ReadFrame(serverConn, protocol.MaxFrameBytes)
		if err != nil || msg.MessageType != protocol.TypeTrafficEvent {
			return
		}
		writeTestSyncTask(t, serverConn, msg)
		taskResult, ok := expectType(t, serverConn, protocol.TypeSyncTaskResult)
		if !ok {
			return
		}
		if taskResult.Sequence != 4 {
			t.Errorf("interleaved task result sequence=%d, want 4", taskResult.Sequence)
			return
		}
		sendAck(serverConn, taskResult)
		body, _ := json.Marshal(protocol.TrafficEventAck{
			AcceptedSequence: msg.Sequence,
			Message:          "ok",
		})
		_ = protocol.WriteFrame(serverConn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "traffic-ack",
			MessageType:     protocol.TypeTrafficEventAck,
			SentAt:          time.Now().UTC(),
			NodeID:          msg.NodeID,
			RequestID:       msg.RequestID,
			ReplyTo:         msg.MessageID,
			Payload:         body,
		})
	}()

	client := Client{NodeID: "node-1", DB: db}
	event := protocol.TrafficEvent{
		EventSequence:   1,
		AuthorizationID: "auth-1",
		NodeRequestID:   "node-req-1",
		MasterRequestID: "master-req-1",
		SentBytes:       12,
		ReportedAt:      time.Now().UTC(),
		AssetID:         "asset-1",
		Status:          "completed",
	}
	next, err := client.sendTrafficEvent(clientConn, "req-1", 3, event)
	if err != nil {
		t.Fatal(err)
	}
	if next != 5 {
		t.Fatalf("traffic next sequence=%d, want 5", next)
	}
	<-done

	var confirmed string
	err = db.QueryRow(`SELECT COALESCE(confirmed_at, '') FROM pending_traffic_events
		WHERE event_sequence = 1`).Scan(&confirmed)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed == "" {
		t.Fatal("流量事件未被确认")
	}
	var pending int
	err = db.QueryRow(`SELECT COUNT(*) FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&pending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("插队任务已直接上报，不应残留 pending result: %d", pending)
	}
}

func TestReadExpectedResponseHandlesInterleavedSyncTaskAck(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		hb, err := protocol.ReadFrame(server, protocol.MaxFrameBytes)
		if err != nil || hb.MessageType != protocol.TypeHeartbeat {
			return
		}
		writeSyncTaskFor(t, server, hb, "task-1")
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok || ack.Sequence != 4 {
			t.Errorf("interleaved ack sequence=%d", ack.Sequence)
			return
		}
		sendAck(server, ack)
		body, _ := json.Marshal(protocol.HeartbeatAckPayload{})
		_ = protocol.WriteFrame(server, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "hb-ack",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          hb.NodeID,
			RequestID:       hb.RequestID,
			ReplyTo:         hb.MessageID,
			Payload:         body,
		})
	}()
	clientCtl := Client{
		NodeID:      "node-1",
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}
	body, _ := json.Marshal(protocol.Heartbeat{Status: "syncing"})
	if err := protocol.WriteFrame(client, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "hb",
		MessageType:     protocol.TypeHeartbeat,
		SentAt:          time.Now().UTC(),
		NodeID:          "node-1",
		RequestID:       "req-1",
		Sequence:        3,
		Payload:         body,
	}); err != nil {
		t.Fatal(err)
	}
	next := uint64(4)
	_, err := clientCtl.readExpectedResponse(client, "req-1", &next, protocol.TypeHeartbeatAck)
	if err != nil {
		t.Fatal(err)
	}
	if next != 5 {
		t.Fatalf("interleaved next sequence=%d, want 5", next)
	}
	<-done
}

func writeTestSyncTask(t *testing.T, conn net.Conn, msg protocol.Envelope) {
	t.Helper()
	body, _ := json.Marshal(protocol.SyncTask{
		TaskID:   "task-1",
		TaskType: "asset_download",
		Asset: protocol.SyncAsset{
			AssetID:  "asset-2",
			FileName: "asset.bin",
		},
	})
	if err := protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "task-1",
		MessageType:     protocol.TypeSyncTask,
		SentAt:          time.Now().UTC(),
		NodeID:          msg.NodeID,
		RequestID:       msg.RequestID,
		Payload:         body,
	}); err != nil {
		t.Error(err)
	}
}
