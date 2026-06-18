package control

import (
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestInventoryReportDoesNotWaitFullOptionalRead(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'asset.bin', 'sha256:abc', 12, 'now', 'verified')`)
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := ackOneMessage(t, server, protocol.TypeInventoryReport)
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}
	var pending *pendingInventoryReport

	start := time.Now()
	next, sent, err := ctl.sendNextInventoryReportChunk(clientConn, "req-1", 7, &pending)
	elapsed := time.Since(start)
	if err != nil || !sent || next != 8 {
		t.Fatalf("inventory next=%d sent=%v err=%v", next, sent, err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("inventory optional read waited too long: %s", elapsed)
	}
	<-done
}
