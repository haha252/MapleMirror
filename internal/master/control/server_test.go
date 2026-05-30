package control

import (
	"encoding/json"
	"net"
	"testing"

	"mirror-server/internal/protocol"
)

func TestWriteStartSessionRejectFrame(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	server := ControlServer{Repo: repo}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.writeStartSessionReject(serverConn, nil, "req-1", "sha256:aa", ErrCertificateNotActive)
		_ = serverConn.Close()
	}()
	msg, err := protocol.ReadFrame(clientConn, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if msg.MessageType != protocol.TypeProtocolError {
		t.Fatalf("期望 protocol_error，实际为 %s", msg.MessageType)
	}
	if msg.NodeID != "sha256:aa" {
		t.Fatalf("期望节点标识回退到 fingerprint，实际为 %q", msg.NodeID)
	}
	var payload protocol.ProtocolError
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "CERTIFICATE_NOT_ACTIVE" {
		t.Fatalf("期望 CERTIFICATE_NOT_ACTIVE，实际为 %q", payload.Code)
	}
	if payload.Message == "" {
		t.Fatal("拒绝消息不应为空")
	}
	<-done
}
