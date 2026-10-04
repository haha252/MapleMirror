package control

import (
	"context"
	"fmt"
	"testing"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2AuthorizationDispatchSelectsDistinctPendingAuthorizations(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedAuthorizationStatusAuth(t, repo, "node-1")
	mustExecControl(t, repo.DB, `UPDATE download_authorizations SET token_hash='hash',max_duration_seconds=600`)
	for i := 2; i <= 6; i++ {
		mustExecControl(t, repo.DB, `INSERT INTO download_authorizations
		 (id,asset_id,node_id,client_prefix_key,issued_at,expires_at,max_bytes,range_limit,status,request_id,token_hash,max_duration_seconds)
		 SELECT ?,asset_id,node_id,client_prefix_key,issued_at,expires_at,max_bytes,range_limit,status,request_id,token_hash,max_duration_seconds
		 FROM download_authorizations WHERE id='auth-1'`, fmt.Sprintf("auth-%d", i))
	}
	s := &V2Server{Repo: repo}
	q := controlv2.NewQueue(512, 4<<20)
	defer q.Close()
	session := Session{NodeID: "node-1"}
	if err := s.dispatchV2Authorizations(context.Background(), session, q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 4 {
		t.Fatalf("dispatched %d distinct authorizations, want 4", count)
	}
	seen := make(map[string]bool)
	var first protocolv2.Envelope
	for i := 0; i < 4; i++ {
		e, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		a, err := protocolv2.Decode[protocolv2.DownloadAuthorization](e)
		if err != nil || seen[a.AuthorizationID] {
			t.Fatalf("duplicate authorization: %+v %v", a, err)
		}
		seen[a.AuthorizationID] = true
		if i == 0 {
			first = e
		}
	}
	if err := s.dispatchV2Authorizations(context.Background(), session, q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 0 {
		t.Fatalf("authorization retransmitted before timeout: %d", count)
	}
	a, _ := protocolv2.Decode[protocolv2.DownloadAuthorization](first)
	ack, _ := protocolv2.Reply(protocolv2.TypeDownloadAuthorizationAck, "ack", first.ID,
		protocolv2.DownloadAuthorizationAck{AuthorizationID: a.AuthorizationID})
	if err := s.handleV2Message(context.Background(), session, q, ack); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchV2Authorizations(context.Background(), session, q); err != nil {
		t.Fatal(err)
	}
	if count, _ := q.Stats(); count != 1 {
		t.Fatalf("ACK did not refill exactly one slot: %d", count)
	}
	next, err := q.Dequeue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := protocolv2.Decode[protocolv2.DownloadAuthorization](next)
	if seen[b.AuthorizationID] {
		t.Fatalf("refill selected an already delivered/in-flight authorization: %s", b.AuthorizationID)
	}
}
