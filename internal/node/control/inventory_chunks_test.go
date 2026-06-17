package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

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
