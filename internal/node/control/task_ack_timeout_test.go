package control

import (
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestReadExpectedResponseTimesOutFasterForTaskAcks(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	clientCtl := Client{NodeID: "node-1"}
	start := time.Now()
	_, err := clientCtl.readExpectedResponse(client, "req-1", nil, protocol.TypeHeartbeatAck)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("ack wait timeout too slow: %s", elapsed)
	}
}
