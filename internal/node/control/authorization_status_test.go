package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestSendAuthorizationStatusEventMarksReportedAfterAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_authorizations
		(authorization_id, asset_id, node_id, issued_at, expires_at, first_seen_at,
		 last_activity_at, status, reason, created_at, updated_at)
		VALUES ('auth-1', 'asset-1', 'node-1', ?, ?, '', ?, 'expired_idle',
		 'expired_idle', ?, ?)`, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := protocol.ReadFrame(serverConn, protocol.MaxFrameBytes)
		if err != nil {
			t.Error(err)
			return
		}
		if msg.MessageType != protocol.TypeAuthorizationStatusEvent {
			t.Errorf("message type=%s", msg.MessageType)
			return
		}
		var event protocol.AuthorizationStatusEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Error(err)
			return
		}
		if event.AuthorizationID != "auth-1" || event.Status != "expired_idle" {
			t.Errorf("event=%+v", event)
		}
		body, _ := json.Marshal(protocol.AuthorizationStatusAck{AcceptedSequence: 7})
		_ = protocol.WriteFrame(serverConn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "ack-1",
			MessageType:     protocol.TypeAuthorizationStatusAck,
			ReplyTo:         msg.MessageID,
			RequestID:       msg.RequestID,
			Payload:         body,
		})
	}()
	client := Client{DB: db, NodeID: "node-1"}
	next, sent, err := client.sendNextAuthorizationStatusEvent(clientConn, "req-1", 7)
	if err != nil || !sent || next != 8 {
		t.Fatalf("send status next=%d sent=%v err=%v", next, sent, err)
	}
	<-done
	var reported string
	if err := db.QueryRow(`SELECT COALESCE(reported_at, '')
		FROM local_authorizations WHERE authorization_id = 'auth-1'`).Scan(&reported); err != nil {
		t.Fatal(err)
	}
	if reported == "" {
		t.Fatal("reported_at should be set after ACK")
	}
}

func TestSendAuthorizationStatusEventDoesNotMarkUpdatedStateReported(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_authorizations
		(authorization_id, asset_id, node_id, issued_at, expires_at, first_seen_at,
		 last_activity_at, status, reason, created_at, updated_at)
		VALUES ('auth-1', 'asset-1', 'node-1', ?, ?, '', ?, 'expired_idle',
		 'expired_idle', ?, ?)`, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	updated := nowTime.Add(time.Second).Format(time.RFC3339Nano)
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, err := protocol.ReadFrame(serverConn, protocol.MaxFrameBytes)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := db.Exec(`UPDATE local_authorizations
			SET status = 'expired_max_duration', reason = 'expired_max_duration',
			updated_at = ?, reported_at = NULL WHERE authorization_id = 'auth-1'`,
			updated); err != nil {
			t.Error(err)
			return
		}
		body, _ := json.Marshal(protocol.AuthorizationStatusAck{AcceptedSequence: 7})
		_ = protocol.WriteFrame(serverConn, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "ack-1",
			MessageType:     protocol.TypeAuthorizationStatusAck,
			ReplyTo:         msg.MessageID,
			RequestID:       msg.RequestID,
			Payload:         body,
		})
	}()
	client := Client{DB: db, NodeID: "node-1"}
	next, sent, err := client.sendNextAuthorizationStatusEvent(clientConn, "req-1", 7)
	if err != nil || !sent || next != 8 {
		t.Fatalf("send status next=%d sent=%v err=%v", next, sent, err)
	}
	<-done
	var status, reported string
	if err := db.QueryRow(`SELECT status, COALESCE(reported_at, '')
		FROM local_authorizations WHERE authorization_id = 'auth-1'`).Scan(&status, &reported); err != nil {
		t.Fatal(err)
	}
	if status != "expired_max_duration" || reported != "" {
		t.Fatalf("updated status should remain pending, status=%s reported=%q", status, reported)
	}
	events, err := client.loadPendingAuthorizationStatusEvents(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Status != "expired_max_duration" {
		t.Fatalf("expected updated status to be sent next: %+v", events)
	}
}
