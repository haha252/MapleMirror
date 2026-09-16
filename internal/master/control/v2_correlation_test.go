package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2SyncAcceptedRequiresTaskReplyTo(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at,lease_expires_at,attempt_id)
		VALUES('task-1',?,'asset-1','asset_download','sent','req','now','now',?,'attempt-1')`, session.NodeID, lease)

	server := &V2Server{Repo: repo}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	body := protocolv2.SyncAccepted{TaskID: "task-1", AttemptID: "attempt-1"}
	wrong, _ := protocolv2.Reply(protocolv2.TypeSyncAccepted, "accepted-1", "wrong", body)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, wrong); err == nil {
		t.Fatal("sync.accepted with wrong reply_to was accepted")
	}
	assertTaskState(t, repo, "task-1", "sent")

	correct, _ := protocolv2.Reply(protocolv2.TypeSyncAccepted, "accepted-2",
		protocolv2.StableMessageID(protocolv2.TypeSyncTask, "task-1", "attempt-1"), body)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, correct); err != nil {
		t.Fatal(err)
	}
	assertTaskState(t, repo, "task-1", "running")
}

func TestV2DownloadAuthorizationAckRequiresAuthorizationReplyTo(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedAuthorizationStatusAuth(t, repo, "node-1")
	session := Session{ID: "sess-1", NodeID: "node-1"}
	server := &V2Server{Repo: repo}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	body := protocolv2.DownloadAuthorizationAck{AuthorizationID: "auth-1"}
	wrong, _ := protocolv2.Reply(protocolv2.TypeDownloadAuthorizationAck, "ack-1", "wrong", body)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, wrong); err == nil {
		t.Fatal("authorization ack with wrong reply_to was accepted")
	}
	assertAuthorizationDelivered(t, repo, false)

	correct, _ := protocolv2.Reply(protocolv2.TypeDownloadAuthorizationAck, "ack-2",
		protocolv2.StableMessageID(protocolv2.TypeDownloadAuthorization, "auth-1"), body)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, correct); err != nil {
		t.Fatal(err)
	}
	assertAuthorizationDelivered(t, repo, true)
}

func TestV2SwarmSourcesRequestRequiresActiveTaskAttempt(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	manifest := testV2Manifest("asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10, 5)
	ack, err := repo.AcceptV2Manifest(context.Background(), session.NodeID, manifest)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("manifest ack=%+v err=%v", ack, err)
	}
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at,lease_expires_at,attempt_id)
		VALUES('task-1',?,'asset-1','asset_download','running','req','now','now',?,'attempt-current')`, session.NodeID, lease)

	server := &V2Server{Repo: repo}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	wrongBody := protocolv2.SwarmSourcesRequest{TaskID: "task-1", AttemptID: "attempt-old", AssetID: "asset-1", ManifestID: manifest.ManifestID}
	wrongID := protocolv2.StableMessageID(protocolv2.TypeSwarmSourcesRequest,
		wrongBody.TaskID, wrongBody.AttemptID, wrongBody.AssetID, wrongBody.ManifestID)
	wrong, _ := protocolv2.New(protocolv2.TypeSwarmSourcesRequest, wrongID, wrongBody)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, wrong); err == nil {
		t.Fatal("stale swarm source request attempt was accepted")
	}

	body := protocolv2.SwarmSourcesRequest{TaskID: "task-1", AttemptID: "attempt-current", AssetID: "asset-1", ManifestID: manifest.ManifestID}
	requestID := protocolv2.StableMessageID(protocolv2.TypeSwarmSourcesRequest,
		body.TaskID, body.AttemptID, body.AssetID, body.ManifestID)
	request, _ := protocolv2.New(protocolv2.TypeSwarmSourcesRequest, requestID, body)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, request); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response.Type != protocolv2.TypeSwarmSources || response.ReplyTo != requestID {
		t.Fatalf("response type=%q reply_to=%q want=%q", response.Type, response.ReplyTo, requestID)
	}
}

func TestV2TrafficEventRequiresStableMessageID(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedTrafficAuth(t, repo)
	session := Session{ID: "sess-1", NodeID: "node-1", CertificateID: "cert-1", RequestID: "req"}
	server := &V2Server{Repo: repo}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	event := protocolv2.TrafficEvent{EventSequence: 7, AuthorizationID: "auth-1", AssetID: "asset-1",
		NodeRequestID: "node-req", MasterRequestID: "master-req-1", SentBytes: 3,
		Status: "completed", ReportedAt: time.Now().UTC()}
	wrong, _ := protocolv2.New(protocolv2.TypeTrafficEvent, "random-id", event)
	if err := server.handleV2BusinessMessage(context.Background(), session, queue, wrong); err == nil {
		t.Fatal("traffic event with random message id was accepted")
	}
	var count int
	if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM traffic_event_dedupe WHERE node_id='node-1' AND event_sequence=7`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("wrongly correlated traffic event mutated accounting: count=%d", count)
	}
}

func assertTaskState(t *testing.T, repo Repository, taskID, want string) {
	t.Helper()
	var got string
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id=?`, taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("task state=%q want=%q", got, want)
	}
}

func assertAuthorizationDelivered(t *testing.T, repo Repository, want bool) {
	t.Helper()
	var delivered string
	if err := repo.DB.QueryRow(`SELECT COALESCE(delivered_at,'') FROM download_authorizations WHERE id='auth-1'`).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if (delivered != "") != want {
		t.Fatalf("delivered_at=%q wantDelivered=%v", delivered, want)
	}
}
