package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunUsesServerInterval(t *testing.T) {
	dialTimes := make(chan time.Time, 2)
	dialer := newPipeDialer(t, func(conn net.Conn) {
		dialTimes <- time.Now()
		handleTestSession(conn)
	})

	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
	}
	stop := make(chan struct{})
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client.Run(stop)
	}()

	first := <-dialTimes
	second := <-dialTimes
	gap := second.Sub(first)
	if gap < 800*time.Millisecond {
		t.Fatalf("客户端重连间隔过短：%s", gap)
	}
	close(stop)
	select {
	case <-clientDone:
	case <-time.After(3 * time.Second):
		t.Fatal("客户端未能停止")
	}
}

func TestHelloReportsConfiguredSoftwareVersion(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		done <- (Client{NodeID: "node-1", SoftwareVersion: "dev-65b3c04"}).hello(client, "req-1")
	}()
	msg, err := protocol.ReadFrame(server, protocol.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	var hello protocol.Hello
	if err := json.Unmarshal(msg.Payload, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.SoftwareVersion != "dev-65b3c04" {
		t.Fatalf("software version=%q", hello.SoftwareVersion)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientRunOnceReturnsProtocolError(t *testing.T) {
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		hello, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || hello.MessageType != protocol.TypeHello {
			return
		}
		body, _ := json.Marshal(protocol.ProtocolError{
			Code:    "CERTIFICATE_NOT_ACTIVE",
			Message: "证书未批准或已失效",
		})
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "reject",
			MessageType:     protocol.TypeProtocolError,
			SentAt:          time.Now().UTC(),
			NodeID:          hello.NodeID,
			RequestID:       hello.RequestID,
			ReplyTo:         hello.MessageID,
			Payload:         body,
		})
	})

	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
	}
	_, err := client.RunOnce()
	if err == nil {
		t.Fatal("期望收到拒绝错误")
	}
	var rejection RejectionError
	if !errors.As(err, &rejection) {
		t.Fatalf("期望拒绝错误，实际为 %T: %v", err, err)
	}
	if rejection.Code != "CERTIFICATE_NOT_ACTIVE" {
		t.Fatalf("期望 CERTIFICATE_NOT_ACTIVE，实际为 %q", rejection.Code)
	}
}

func TestClientRunOnceKeepsSendingHeartbeatsOnOneConnection(t *testing.T) {
	heartbeats := make(chan uint64, 2)
	done := make(chan struct{})
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer close(done)
		handleLongSession(t, conn, heartbeats)
	})

	client := Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := client.RunOnce()
		errCh <- err
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("fake master did not receive repeated heartbeats")
	}
	if len(heartbeats) < 2 {
		t.Fatalf("expected at least two heartbeats on one connection, got %d", len(heartbeats))
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected RunOnce to finish with connection close error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not leave closed control connection")
	}
}

func handleTestSession(conn net.Conn) {
	defer conn.Close()
	hello, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil || hello.MessageType != protocol.TypeHello {
		return
	}
	welcomeBody, _ := json.Marshal(protocol.Welcome{
		HeartbeatIntervalSecond: 1,
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
}

func handleLongSession(t *testing.T, conn net.Conn, heartbeats chan<- uint64) {
	t.Helper()
	defer conn.Close()
	hello, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil || hello.MessageType != protocol.TypeHello {
		return
	}
	welcomeBody, _ := json.Marshal(protocol.Welcome{
		HeartbeatIntervalSecond: 1,
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
	for len(heartbeats) < 2 {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			return
		}
		switch msg.MessageType {
		case protocol.TypeHeartbeat:
			heartbeats <- msg.Sequence
			sendAck(conn, msg)
		case protocol.TypePressureReport:
			sendAck(conn, msg)
		default:
			sendAck(conn, msg)
		}
	}
}

func newPipeDialer(t *testing.T, handler func(net.Conn)) func(context.Context, string, string, *tls.Config) (net.Conn, error) {
	t.Helper()
	return func(context.Context, string, string, *tls.Config) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			handler(server)
		}()
		return client, nil
	}
}

func runOnceEndedByPeer(err error) bool {
	return err == nil || errors.Is(err, io.EOF) ||
		strings.Contains(err.Error(), "closed pipe")
}
