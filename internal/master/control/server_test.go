package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

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

func TestReadHelloRejectsUnexpectedControlMessage(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	server := ControlServer{Repo: repo, HeartbeatTimeout: time.Second}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	session := Session{ID: "sess-1", NodeID: "node-1"}
	done := make(chan error, 1)
	go func() {
		done <- server.readHello(serverConn, session, "req-1")
		_ = serverConn.Close()
	}()
	body, _ := json.Marshal(protocol.Heartbeat{Status: "syncing"})
	if err := protocol.WriteFrame(clientConn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "msg-1",
		MessageType:     protocol.TypeHeartbeat,
		SentAt:          time.Now().UTC(),
		NodeID:          session.NodeID,
		RequestID:       "req-node",
		Payload:         body,
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := protocol.ReadFrame(clientConn, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if msg.MessageType != protocol.TypeProtocolError {
		t.Fatalf("期望 protocol_error，实际为 %s", msg.MessageType)
	}
	var payload protocol.ProtocolError
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "CONTROL_PROTOCOL_ERROR" || payload.Message == "" {
		t.Fatalf("拒绝载荷不正确：%+v", payload)
	}
	if err := <-done; err == nil {
		t.Fatal("期望 hello 交换失败")
	}
}
