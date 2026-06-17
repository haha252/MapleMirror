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
	if _, err := client.RunOnce(); !runOnceEndedByPeer(err) {
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
	nextSeq, err := ctl.sendFullInventoryReport(client, "req-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if nextSeq != 5 {
		t.Fatalf("expected next sequence 5, got %d", nextSeq)
	}
	<-done
}

func TestPartialInventoryReportDoesNotReadOptionalTasksBeforeCompletion(t *testing.T) {
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
		msg, ok := expectType(t, server, protocol.TypeInventoryReport)
		if !ok {
			return
		}
		var report protocol.InventoryReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			t.Error(err)
			return
		}
		if report.Complete {
			t.Errorf("first inventory chunk should not be complete")
			return
		}
		if report.SyncTaskSlotsAvailable != nil {
			t.Errorf("partial inventory should not advertise dispatch capacity, got %+v", report.SyncTaskSlotsAvailable)
			return
		}
		sendAck(server, msg)
		if err := server.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
			t.Error(err)
			return
		}
		if _, err := protocol.ReadFrame(server, protocol.MaxFrameBytes); err == nil {
			t.Error("partial inventory should not trigger optional task reads")
			return
		}
	}()
	ctl := &Client{NodeID: "node-1", DB: db}
	var pending *pendingInventoryReport
	_, _, err := ctl.sendNextInventoryReportChunk(client, "req-1", 3, &pending)
	if err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestPrepareInventoryReportUsesStoredSnapshot(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-missing', 'missing.bin', 'sha256:aa', 1, 'now', 'verified')`)
	ctl := &Client{NodeID: "node-1", DB: db, Storage: t.TempDir()}

	report, err := ctl.prepareInventoryReport()
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || len(report.Chunks) != 1 || len(report.Chunks[0]) != 1 {
		t.Fatalf("unexpected inventory snapshot: %+v", report)
	}
	if report.Chunks[0][0].LocalState != "verified" {
		t.Fatalf("control report should use stored state, got %q", report.Chunks[0][0].LocalState)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'asset-missing'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "verified" {
		t.Fatalf("control report should not mutate local inventory state, got %q", state)
	}
}

func TestInventoryReconcileResultForcesInventoryReportDue(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at, force_report_requested_at)
		VALUES (1, 2, 1, ?, 'force-now')`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	ctl := &Client{NodeID: "node-1", DB: db}
	if report, err := ctl.prepareInventoryReport(); err != nil || report == nil {
		t.Fatalf("forced inventory cursor should be due: report=%+v err=%v", report, err)
	}
}

func TestCompleteInventoryReportClearsForceRequest(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'asset.bin', 'sha256:abc', 12, 'now', 'verified')`)
	if _, err := db.Exec(`INSERT INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at, force_report_requested_at)
		VALUES (1, 1, 0, ?, 'force-now')`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, server, protocol.TypeInventoryReport)
		if !ok {
			return
		}
		var report protocol.InventoryReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			t.Error(err)
			return
		}
		if !report.Complete {
			t.Error("forced inventory report should complete in one chunk")
			return
		}
		sendAck(server, msg)
	}()
	ctl := &Client{NodeID: "node-1", DB: db}
	if _, err := ctl.sendFullInventoryReport(client, "req-1", 3); err != nil {
		t.Fatal(err)
	}
	<-done
	var force string
	if err := db.QueryRow(`SELECT COALESCE(force_report_requested_at, '')
		FROM inventory_report_cursor WHERE id = 1`).Scan(&force); err != nil {
		t.Fatal(err)
	}
	if force != "" {
		t.Fatalf("force request should be cleared after full report, got %q", force)
	}
}

func TestInventoryReconcileForceSurvivesInFlightInventoryCompletion(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	for i := 0; i < 1001; i++ {
		mustExecNode(t, db, fmt.Sprintf(`INSERT INTO local_assets
			(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
			VALUES ('asset-%04d', 'asset.bin', 'sha256:aa', 1, 'now', 'verified')`, i))
	}
	mustExecNode(t, db, `INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-reconcile', NULL, 'inventory_reconcile', 'running', 'now')`)
	ctl := &Client{NodeID: "node-1", DB: db}
	inFlight, err := ctl.prepareInventoryReport()
	if err != nil {
		t.Fatal(err)
	}
	if inFlight == nil || len(inFlight.Chunks) != 2 || inFlight.ForceRequestedAt != "" {
		t.Fatalf("unexpected initial inventory report: %+v", inFlight)
	}
	if err := ctl.storePendingTaskResult(protocol.SyncTaskResult{
		TaskID: "task-reconcile",
		Result: "succeeded",
	}); err != nil {
		t.Fatal(err)
	}
	inFlight.Index = len(inFlight.Chunks)
	if err := ctl.storeInventoryCursor(inventoryCursor{
		NextRevision:      inFlight.Revision + 1,
		LastAckedRevision: inFlight.Revision,
		ForceRequestedAt:  inFlight.ForceRequestedAt,
	}); err != nil {
		t.Fatal(err)
	}

	forced, err := ctl.prepareInventoryReport()
	if err != nil {
		t.Fatal(err)
	}
	if forced == nil {
		t.Fatal("in-flight inventory completion swallowed forced reconcile inventory")
	}
	if forced.Revision != 2 {
		t.Fatalf("forced inventory revision = %d, want 2", forced.Revision)
	}
	if forced.ForceRequestedAt == "" {
		t.Fatal("forced inventory report should remember the request it consumes")
	}
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
