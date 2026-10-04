package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2TrafficReplayWaitsForAckInsteadOfRepeatingBurst(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 1; i <= 71; i++ {
		if _, err := db.Exec(`INSERT INTO pending_traffic_events
		 (event_sequence,authorization_id,node_request_id,master_request_id,sent_bytes,created_at,asset_id,status)
		 VALUES(?,'auth','node-req','master-req',10,?,'asset','completed')`, i, now); err != nil {
			t.Fatal(err)
		}
	}
	c := &Client{DB: db}
	q := controlv2.NewQueue(512, 4<<20)
	defer q.Close()
	if err := c.enqueuePendingV2Traffic(q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 4 {
		t.Fatalf("reconnect replay sent %d messages, want window of 4", count)
	}
	for i := 0; i < 4; i++ {
		if _, err := q.Dequeue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.enqueuePendingV2Traffic(q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 0 {
		t.Fatalf("unacknowledged messages replayed again: %d", count)
	}
}

func TestV2StatusACKConfirmsFrozenVersionWithoutDisconnectingOnLateACK(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO local_authorizations
	 (authorization_id,asset_id,node_id,issued_at,expires_at,status,reason,created_at,updated_at)
	 VALUES('auth','asset','node',?,?,'expired_idle','old',?,?)`, stamp, stamp, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{DB: db}
	q := controlv2.NewQueue(32, 1<<20)
	defer q.Close()
	if err := c.enqueuePendingV2AuthorizationStatus(q); err != nil {
		t.Fatal(err)
	}
	e, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE local_authorizations SET reason='new',updated_at=? WHERE authorization_id='auth'`, now.Add(time.Second).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	ack, _ := protocolv2.Reply(protocolv2.TypeAuthorizationStatusAck, "ack", e.ID, protocolv2.AuthorizationStatusAck{AuthorizationID: "auth", Status: "expired_idle"})
	if err := c.handleV2Inbound(context.Background(), q, ack); err != nil {
		t.Fatal(err)
	}
	if err := c.handleV2Inbound(context.Background(), q, ack); err != nil {
		t.Fatalf("duplicate late ACK disconnected: %v", err)
	}
	var reported string
	if err := db.QueryRow(`SELECT COALESCE(reported_at,'') FROM local_authorizations WHERE authorization_id='auth'`).Scan(&reported); err != nil || reported != "" {
		t.Fatalf("old ACK confirmed newer status: %q %v", reported, err)
	}
	if err := c.enqueuePendingV2AuthorizationStatus(q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 1 {
		t.Fatalf("new version did not refill window: %d", count)
	}
}
