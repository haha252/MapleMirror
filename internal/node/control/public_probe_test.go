package control

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestClientRunOnceSendsPublicProbeReadyBeforePressureReport(t *testing.T) {
	accepted := make(chan protocol.PublicProbeChallenge, 1)
	readySeen := make(chan protocol.PublicProbeReady, 1)
	dialer := newPipeDialer(t, func(conn net.Conn) {
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
		challenge := protocol.PublicProbeChallenge{
			ChallengeID: "probe-1",
			Nonce:       "nonce-1",
			ExpiresAt:   time.Now().Add(time.Minute).UTC(),
			Algorithm:   "ecdsa-p256-sha256",
		}
		ackBody, _ := json.Marshal(protocol.HeartbeatAckPayload{
			AcceptedSequence: hb.Sequence,
			ManagedState:     "syncing",
			RoutingReady:     false,
			PublicProbe:      &challenge,
		})
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "ack-heartbeat",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          hb.NodeID,
			RequestID:       hb.RequestID,
			ReplyTo:         hb.MessageID,
			Payload:         ackBody,
		})

		ready, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || ready.MessageType != protocol.TypePublicProbeReady {
			return
		}
		var payload protocol.PublicProbeReady
		if err := json.Unmarshal(ready.Payload, &payload); err == nil {
			readySeen <- payload
		}
		sendAck(conn, ready)

		pressure, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || pressure.MessageType != protocol.TypePressureReport {
			return
		}
		sendAck(conn, pressure)
	})

	client := Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		ProbeStore: publicProbeAcceptFunc(func(challenge protocol.PublicProbeChallenge) error {
			accepted <- challenge
			return nil
		}),
	}
	err := runOnceExpectPeerClose(&client)
	if err != nil {
		t.Fatalf("RunOnce ended unexpectedly: %v", err)
	}

	select {
	case challenge := <-accepted:
		if challenge.ChallengeID != "probe-1" {
			t.Fatalf("accepted challenge=%q", challenge.ChallengeID)
		}
	default:
		t.Fatal("expected probe challenge to be accepted locally")
	}
	select {
	case ready := <-readySeen:
		if ready.ChallengeID != "probe-1" {
			t.Fatalf("ready challenge=%q", ready.ChallengeID)
		}
	default:
		t.Fatal("expected public_probe_ready control message")
	}
}

func TestClientRetriesPendingPublicProbeReadyOnNextConnection(t *testing.T) {
	var attempts int32
	readySeen := make(chan string, 2)
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		connection := atomic.AddInt32(&attempts, 1)
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
		ack := protocol.HeartbeatAckPayload{
			AcceptedSequence: hb.Sequence,
			ManagedState:     "syncing",
			RoutingReady:     false,
		}
		if connection == 1 {
			ack.PublicProbe = &protocol.PublicProbeChallenge{
				ChallengeID: "probe-retry",
				Nonce:       "nonce-1",
				ExpiresAt:   time.Now().Add(time.Minute).UTC(),
				Algorithm:   "ecdsa-p256-sha256",
			}
		}
		ackBody, _ := json.Marshal(ack)
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "ack-heartbeat",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          hb.NodeID,
			RequestID:       hb.RequestID,
			ReplyTo:         hb.MessageID,
			Payload:         ackBody,
		})

		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil {
			return
		}
		if msg.MessageType != protocol.TypePublicProbeReady {
			t.Errorf("connection %d expected ready before pressure, got %s", connection, msg.MessageType)
			return
		}
		var ready protocol.PublicProbeReady
		if err := json.Unmarshal(msg.Payload, &ready); err == nil {
			readySeen <- ready.ChallengeID
		}
		if connection == 1 {
			return
		}
		sendAck(conn, msg)

		pressure, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil || pressure.MessageType != protocol.TypePressureReport {
			t.Errorf("expected pressure after retried ready, got %v %s", err, pressure.MessageType)
			return
		}
		sendAck(conn, pressure)
	})

	client := Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		ProbeStore:     publicProbeAcceptFunc(func(protocol.PublicProbeChallenge) error { return nil }),
	}
	if err := runOnceExpectPeerClose(&client); err != nil {
		t.Fatalf("first RunOnce ended unexpectedly: %v", err)
	}
	if err := runOnceExpectPeerClose(&client); err != nil {
		t.Fatalf("second RunOnce ended unexpectedly: %v", err)
	}
	if got := len(readySeen); got != 2 {
		t.Fatalf("expected two ready attempts, got %d", got)
	}
}

func TestClientDoesNotResendAckedPublicProbeReady(t *testing.T) {
	var connections int32
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		connection := atomic.AddInt32(&connections, 1)
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
		ack := protocol.HeartbeatAckPayload{
			AcceptedSequence: hb.Sequence,
			ManagedState:     "syncing",
			RoutingReady:     false,
		}
		if connection == 1 {
			ack.PublicProbe = &protocol.PublicProbeChallenge{
				ChallengeID: "probe-once",
				Nonce:       "nonce-1",
				ExpiresAt:   time.Now().Add(time.Minute).UTC(),
				Algorithm:   "ecdsa-p256-sha256",
			}
		}
		ackBody, _ := json.Marshal(ack)
		_ = protocol.WriteFrame(conn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "ack-heartbeat",
			MessageType:     protocol.TypeHeartbeatAck,
			SentAt:          time.Now().UTC(),
			NodeID:          hb.NodeID,
			RequestID:       hb.RequestID,
			ReplyTo:         hb.MessageID,
			Payload:         ackBody,
		})

		msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
		if err != nil {
			return
		}
		if ack.PublicProbe != nil {
			if msg.MessageType != protocol.TypePublicProbeReady {
				t.Errorf("expected ready on first connection, got %s", msg.MessageType)
				return
			}
			sendAck(conn, msg)
			msg, err = protocol.ReadFrame(conn, protocol.MaxFrameBytes)
			if err != nil {
				return
			}
		}
		if msg.MessageType != protocol.TypePressureReport {
			t.Errorf("expected pressure without duplicate ready, got %s", msg.MessageType)
			return
		}
		sendAck(conn, msg)
	})

	client := Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		ProbeStore:     publicProbeAcceptFunc(func(protocol.PublicProbeChallenge) error { return nil }),
	}
	if err := runOnceExpectPeerClose(&client); err != nil {
		t.Fatalf("first RunOnce ended unexpectedly: %v", err)
	}
	if err := runOnceExpectPeerClose(&client); err != nil {
		t.Fatalf("second RunOnce ended unexpectedly: %v", err)
	}
}

func TestClientDropsExpiredPendingPublicProbeReady(t *testing.T) {
	client := Client{
		pendingPublicProbeReady: map[string]pendingPublicProbeReady{
			"probe-expired": {
				ChallengeID:   "probe-expired",
				ExpiresAt:     time.Now().Add(-time.Second),
				NextAttemptAt: time.Now().Add(-time.Second),
			},
		},
	}
	next, sent, err := client.flushPendingPublicProbeReady(nil, "req-1", 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if sent || next != 7 {
		t.Fatalf("expired ready should not be sent: sent=%v next=%d", sent, next)
	}
	if len(client.pendingPublicProbeReady) != 0 {
		t.Fatalf("expired ready should be removed, got %+v", client.pendingPublicProbeReady)
	}
}

func runOnceExpectPeerClose(client *Client) error {
	_, err := client.RunOnce()
	if runOnceEndedByPeer(err) {
		return nil
	}
	return err
}

type publicProbeAcceptFunc func(protocol.PublicProbeChallenge) error

func (f publicProbeAcceptFunc) Accept(challenge protocol.PublicProbeChallenge) error {
	return f(challenge)
}
