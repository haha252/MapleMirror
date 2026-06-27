package control

import (
	"crypto/tls"
	"encoding/json"
	"net"
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
