package control

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunOnceReportsInventoryAfterPendingTaskResults(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', 'now')`)
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'asset.bin', 'sha256:abc', 12, 'now', 'verified')`)

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
		taskResult, ok := expectType(t, conn, protocol.TypeSyncTaskResult)
		if !ok {
			return
		}
		sendAck(conn, taskResult)
		inventory, ok := expectType(t, conn, protocol.TypeInventoryReport)
		if !ok {
			return
		}
		var report protocol.InventoryReport
		if err := json.Unmarshal(inventory.Payload, &report); err != nil {
			t.Error(err)
			return
		}
		if !report.Complete || len(report.Items) != 1 || report.Items[0].AssetID != "asset-1" {
			t.Errorf("unexpected inventory report: %+v", report)
			return
		}
		sendAck(conn, inventory)
	})

	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		DB:             db,
	}
	if _, err := client.RunOnce(); err != nil {
		t.Fatal(err)
	}
	var acked uint64
	if err := db.QueryRow(`SELECT last_acked_revision FROM inventory_report_cursor WHERE id = 1`).Scan(&acked); err != nil {
		t.Fatal(err)
	}
	if acked != 1 {
		t.Fatalf("expected acked revision 1, got %d", acked)
	}
}

func TestSendFullInventoryReportSplitsIntoChunks(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	for i := 0; i < 1001; i++ {
		mustExecNode(t, db, fmt.Sprintf(`INSERT INTO local_assets
			(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
			VALUES ('asset-%04d', 'a.bin', 'sha256:aa', 1, 'now', 'verified')`, i))
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for idx, expected := range []int{1000, 1} {
			msg, ok := expectType(t, server, protocol.TypeInventoryReport)
			if !ok {
				return
			}
			var report protocol.InventoryReport
			if err := json.Unmarshal(msg.Payload, &report); err != nil {
				t.Error(err)
				return
			}
			if len(report.Items) != expected {
				t.Errorf("chunk %d expected %d items, got %d", idx, expected, len(report.Items))
				return
			}
			if report.Revision != 1 {
				t.Errorf("chunk %d expected revision 1, got %d", idx, report.Revision)
				return
			}
			if report.Complete != (idx == 1) {
				t.Errorf("chunk %d complete mismatch", idx)
				return
			}
			sendAck(server, msg)
		}
	}()
	ctl := &Client{NodeID: "node-1", DB: db}
	nextSeq, err := ctl.sendFullInventoryReport(client, "req-1", 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if nextSeq != 5 {
		t.Fatalf("expected next sequence 5, got %d", nextSeq)
	}
	<-done
}

func expectType(t *testing.T, conn interface{ Read([]byte) (int, error) }, typ string) (protocol.Envelope, bool) {
	t.Helper()
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil {
		t.Error(err)
		return protocol.Envelope{}, false
	}
	if msg.MessageType != typ {
		t.Errorf("expected %s, got %s", typ, msg.MessageType)
		return protocol.Envelope{}, false
	}
	return msg, true
}

func sendWelcome(conn interface{ Write([]byte) (int, error) }, hello protocol.Envelope) {
	welcomeBody, _ := json.Marshal(protocol.Welcome{
		HeartbeatIntervalSecond: 10,
		HeartbeatTimeoutSecond:  30,
		ManagedState:            "syncing",
		RoutingReady:            false,
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "welcome",
		MessageType:     protocol.TypeWelcome,
		SentAt:          time.Now().UTC(),
		NodeID:          hello.NodeID,
		RequestID:       hello.RequestID,
		ReplyTo:         hello.MessageID,
		Payload:         welcomeBody,
	})
}

func sendAck(conn interface{ Write([]byte) (int, error) }, msg protocol.Envelope) {
	ackBody, _ := json.Marshal(map[string]any{
		"accepted_sequence": msg.Sequence,
		"managed_state":     "syncing",
		"routing_ready":     false,
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "ack",
		MessageType:     protocol.TypeHeartbeatAck,
		SentAt:          time.Now().UTC(),
		NodeID:          msg.NodeID,
		RequestID:       msg.RequestID,
		ReplyTo:         msg.MessageID,
		Payload:         ackBody,
	})
}

func mustExecNode(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
