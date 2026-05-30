package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunUsesServerInterval(t *testing.T) {
	ln := testTLSServer(t)
	defer ln.Close()

	acceptTimes := make(chan time.Time, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			acceptTimes <- time.Now()
			go handleTestSession(conn)
		}
	}()

	client := &Client{
		NodeID:    "node-1",
		Address:   ln.Addr().String(),
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}
	stop := make(chan struct{})
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client.Run(stop)
	}()

	first := <-acceptTimes
	second := <-acceptTimes
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
	<-done
}

func TestClientRunOnceReturnsProtocolError(t *testing.T) {
	ln := testTLSServer(t)
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
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
	}()

	client := &Client{
		NodeID:    "node-1",
		Address:   ln.Addr().String(),
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
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
	<-done
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
	ackBody, _ := json.Marshal(map[string]any{
		"accepted_sequence": hb.Sequence,
		"managed_state":     "syncing",
		"routing_ready":     false,
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "heartbeat-ack",
		MessageType:     protocol.TypeHeartbeatAck,
		SentAt:          time.Now().UTC(),
		NodeID:          hb.NodeID,
		RequestID:       hb.RequestID,
		ReplyTo:         hb.MessageID,
		Payload:         ackBody,
	})
}

func testTLSServer(t *testing.T) net.Listener {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        tpl,
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	return ln
}
