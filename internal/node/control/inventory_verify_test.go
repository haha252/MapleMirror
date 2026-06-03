package control

import (
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/protocol"
)

func TestSendFullInventoryReportMarksMissingFile(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'missing.bin', 'sha256:abc', 12, 'now', 'verified')`)
	report := sendInventoryReportForTest(t, db, t.TempDir())
	if len(report.Items) != 1 || report.Items[0].LocalState != "missing" {
		t.Fatalf("expected missing inventory item, got %+v", report.Items)
	}
	var state string
	err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'asset-1'`).Scan(&state)
	if err != nil || state != "missing" {
		t.Fatalf("local asset state got=%q err=%v", state, err)
	}
}

func TestSendFullInventoryReportRestoresVerifiedFile(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "asset.bin"), []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'asset.bin',
		'sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
		3, 'now', 'missing')`)
	report := sendInventoryReportForTest(t, db, dir)
	if len(report.Items) != 1 || report.Items[0].LocalState != "verified" {
		t.Fatalf("expected verified inventory item, got %+v", report.Items)
	}
}

func sendInventoryReportForTest(t *testing.T, db *sql.DB, storage string) protocol.InventoryReport {
	t.Helper()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan protocol.InventoryReport)
	go func() {
		msg, ok := expectType(t, server, protocol.TypeInventoryReport)
		if !ok {
			close(done)
			return
		}
		var report protocol.InventoryReport
		if err := json.Unmarshal(msg.Payload, &report); err != nil {
			t.Error(err)
			close(done)
			return
		}
		sendAck(server, msg)
		done <- report
	}()
	ctl := &Client{NodeID: "node-1", DB: db, Storage: storage}
	if _, err := ctl.sendFullInventoryReport(client, "req-1", 3); err != nil {
		t.Fatal(err)
	}
	return <-done
}
