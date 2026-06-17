package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

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
