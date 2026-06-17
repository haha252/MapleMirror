package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestPartialInventoryReportCanReadOptionalTasksBetweenChunks(t *testing.T) {
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
	executed := make(chan string, 1)
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
		writeSyncTaskFor(t, server, msg, "task-between-chunks")
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		if ack.Sequence != 4 {
			t.Errorf("sync task ack sequence = %d, want 4", ack.Sequence)
			return
		}
		sendAck(server, ack)
	}()
	ctl := &Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: executed},
		TaskLimiter: NewTaskLimiter(1),
	}
	var pending *pendingInventoryReport
	next, sent, err := ctl.sendNextInventoryReportChunk(client, "req-1", 3, &pending)
	if err != nil {
		t.Fatal(err)
	}
	if !sent || next != 5 {
		t.Fatalf("chunk should send and read task, sent=%v next=%d", sent, next)
	}
	<-done
	select {
	case id := <-executed:
		if id != "task-between-chunks" {
			t.Fatalf("executed task = %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("task between inventory chunks was not executed")
	}
}
