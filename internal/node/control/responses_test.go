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
	if err := client.sendTrafficEvent(clientConn, "req-1", 3, event); err != nil {
		t.Fatal(err)
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
	var result string
	err = db.QueryRow(`SELECT result FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	if result != "temporary_error" {
		t.Fatalf("期望任务暂存为 temporary_error，实际为 %q", result)
	}
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
