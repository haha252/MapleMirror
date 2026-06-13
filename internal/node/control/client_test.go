package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
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
