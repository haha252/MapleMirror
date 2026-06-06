package control

import (
	"crypto/tls"
	"encoding/json"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunOnceReportsPendingTaskResultBeforeReadingNewTask(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	ln := testTLSServer(t)
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		hello, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || hello.MessageType != protocol.TypeHello {
			return
		}
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
		hb, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || hb.MessageType != protocol.TypeHeartbeat {
			return
		}
		sendAck(conn, hb)
		pressure, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || pressure.MessageType != protocol.TypePressureReport {
			return
		}
		sendAck(conn, pressure)
		result, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || result.MessageType != protocol.TypeSyncTaskResult {
			return
		}
		sendAck(conn, result)
		report, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || report.MessageType != protocol.TypeInventoryReport {
			return
		}
		sendAck(conn, report)
	}()
	client := &Client{
		NodeID:    "node-1",
		Address:   ln.Addr().String(),
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
		DB:        db,
	}
	if _, err := client.RunOnce(); err != nil {
		t.Fatal(err)
	}
	var reportedAt string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '') FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&reportedAt); err != nil {
		t.Fatal(err)
	}
	if reportedAt == "" {
		t.Fatal("待上报同步结果未标记为已上报")
	}
	<-done
}
