package control

import (
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2ResultAckRequiresExactReplyTo(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO pending_sync_task_results
		(task_id,asset_id,result,local_digest_sha256,size_bytes,message,created_at,attempt_id)
		VALUES('task-1','asset-1','succeeded','sha256:aa',10,'ok',?,'attempt-1')`, now); err != nil {
		t.Fatal(err)
	}
	client := &Client{DB: db}
	body := protocolv2.SyncResultAck{TaskID: "task-1", AttemptID: "attempt-1", Accepted: true}
	wrong, _ := protocolv2.Reply(protocolv2.TypeSyncResultAck, "ack-1", "wrong", body)
	if err := client.handleV2ResultAck(wrong); err == nil {
		t.Fatal("wrong reply_to accepted")
	}
	assertPendingResultReported(t, db, false)

	correct, _ := protocolv2.Reply(protocolv2.TypeSyncResultAck, "ack-2",
		protocolv2.StableMessageID(protocolv2.TypeSyncResult, "task-1", "attempt-1"), body)
	if err := client.handleV2ResultAck(correct); err != nil {
		t.Fatal(err)
	}
	assertPendingResultReported(t, db, true)
	if err := client.handleV2ResultAck(correct); err != nil {
		t.Fatalf("duplicate exact result ack must be idempotent: %v", err)
	}
}

func TestV2TrafficAckRequiresExactReplyTo(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO pending_traffic_events
		(event_sequence,authorization_id,node_request_id,master_request_id,sent_bytes,created_at,asset_id,status)
		VALUES(7,'auth-1','node-req','master-req',10,?,'asset-1','completed')`, now); err != nil {
		t.Fatal(err)
	}
	client := &Client{DB: db}
	body := protocolv2.TrafficEventAck{EventSequence: 7}
	wrong, _ := protocolv2.Reply(protocolv2.TypeTrafficEventAck, "ack-1", "wrong", body)
	if err := client.handleV2TrafficAck(wrong); err == nil {
		t.Fatal("wrong traffic reply_to accepted")
	}
	assertTrafficConfirmed(t, db, false)

	correct, _ := protocolv2.Reply(protocolv2.TypeTrafficEventAck, "ack-2",
		protocolv2.StableMessageID(protocolv2.TypeTrafficEvent, "7"), body)
	if err := client.handleV2TrafficAck(correct); err != nil {
		t.Fatal(err)
	}
	assertTrafficConfirmed(t, db, true)
}

func TestV2AuthorizationStatusAckBindsExactPendingVersion(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_authorizations
		(authorization_id,asset_id,node_id,issued_at,expires_at,first_seen_at,last_activity_at,
		status,reason,created_at,updated_at)
		VALUES('auth-1','asset-1','node-1',?,?,'',?,'expired_idle','reason-a',?,?)`,
		now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	client := &Client{DB: db}
	body := protocolv2.AuthorizationStatusAck{AuthorizationID: "auth-1", Status: "expired_idle"}
	oldID := protocolv2.StableMessageID(protocolv2.TypeAuthorizationStatus,
		"auth-1", "expired_idle", "reason-a", now)

	updated := nowTime.Add(time.Second).Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE local_authorizations SET reason='reason-b',updated_at=? WHERE authorization_id='auth-1'`, updated); err != nil {
		t.Fatal(err)
	}
	stale, _ := protocolv2.Reply(protocolv2.TypeAuthorizationStatusAck, "ack-1", oldID, body)
	if err := client.handleV2AuthorizationStatusAck(stale); err == nil {
		t.Fatal("stale authorization status ack accepted")
	}
	assertAuthorizationReported(t, db, false)

	currentID := protocolv2.StableMessageID(protocolv2.TypeAuthorizationStatus,
		"auth-1", "expired_idle", "reason-b", updated)
	correct, _ := protocolv2.Reply(protocolv2.TypeAuthorizationStatusAck, "ack-2", currentID, body)
	if err := client.handleV2AuthorizationStatusAck(correct); err != nil {
		t.Fatal(err)
	}
	assertAuthorizationReported(t, db, true)
}

func TestV2InventoryAckRequiresFinalSegmentReplyTo(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	runtime := NewV2Runtime()
	runtime.inventoryPending = 3
	runtime.inventoryPendingID = protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, "3", "2")
	client := &Client{DB: db, V2Runtime: runtime}
	body := protocolv2.InventorySnapshotAck{Revision: 3}
	wrong, _ := protocolv2.Reply(protocolv2.TypeInventorySnapshotAck, "ack-1",
		protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, "3", "1"), body)
	if err := client.handleV2InventoryAck(wrong); err == nil {
		t.Fatal("ack for non-final inventory segment accepted")
	}
	if runtime.inventoryPending != 3 {
		t.Fatalf("pending revision changed after wrong ack: %d", runtime.inventoryPending)
	}

	correct, _ := protocolv2.Reply(protocolv2.TypeInventorySnapshotAck, "ack-2", runtime.inventoryPendingID, body)
	if err := client.handleV2InventoryAck(correct); err != nil {
		t.Fatal(err)
	}
	if runtime.inventoryPending != 0 || runtime.inventoryPendingID != "" {
		t.Fatalf("inventory correlation state not cleared: revision=%d id=%q", runtime.inventoryPending, runtime.inventoryPendingID)
	}
}

func assertPendingResultReported(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	var reported string
	if err := db.QueryRow(`SELECT COALESCE(reported_at,'') FROM pending_sync_task_results WHERE task_id='task-1'`).Scan(&reported); err != nil {
		t.Fatal(err)
	}
	if (reported != "") != want {
		t.Fatalf("reported=%q wantReported=%v", reported, want)
	}
}

func assertTrafficConfirmed(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	var confirmed string
	if err := db.QueryRow(`SELECT COALESCE(confirmed_at,'') FROM pending_traffic_events WHERE event_sequence=7`).Scan(&confirmed); err != nil {
		t.Fatal(err)
	}
	if (confirmed != "") != want {
		t.Fatalf("confirmed=%q wantConfirmed=%v", confirmed, want)
	}
}

func assertAuthorizationReported(t *testing.T, db *sql.DB, want bool) {
	t.Helper()
	var reported string
	if err := db.QueryRow(`SELECT COALESCE(reported_at,'') FROM local_authorizations WHERE authorization_id='auth-1'`).Scan(&reported); err != nil {
		t.Fatal(err)
	}
	if (reported != "") != want {
		t.Fatalf("reported=%q wantReported=%v", reported, want)
	}
}

func TestV2ManifestAckRequiresExactReplyTo(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	client := &Client{DB: db}
	manifest := protocolv2.SwarmManifest{ManifestID: "manifest-1", AssetID: "asset-1"}
	result := protocolv2.SyncResult{TaskID: "task-1", AttemptID: "attempt-1", AssetID: "asset-1", Result: "succeeded"}
	if err := client.storePendingV2Manifest(manifest, result); err != nil {
		t.Fatal(err)
	}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	body := protocolv2.SwarmManifestAck{ManifestID: "manifest-1", AssetID: "asset-1", Status: "accepted"}
	wrong, _ := protocolv2.Reply(protocolv2.TypeSwarmManifestAck, "ack-1", "wrong", body)
	if err := client.handleV2ManifestAck(queue, wrong); err == nil {
		t.Fatal("manifest ack with wrong reply_to accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_swarm_manifests WHERE manifest_id='manifest-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("manifest outbox consumed after wrong ack: count=%d", count)
	}

	correct, _ := protocolv2.Reply(protocolv2.TypeSwarmManifestAck, "ack-2",
		protocolv2.StableMessageID(protocolv2.TypeSwarmManifestReport, "manifest-1"), body)
	if err := client.handleV2ManifestAck(queue, correct); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_swarm_manifests WHERE manifest_id='manifest-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("manifest outbox not consumed after exact ack: count=%d", count)
	}
	var resultCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_sync_task_results
		WHERE task_id='task-1' AND attempt_id='attempt-1' AND reported_at IS NULL`).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if resultCount != 1 {
		t.Fatalf("terminal result must become durable only after manifest ack: count=%d", resultCount)
	}
}

func TestV2SourcesResponseRequiresRequestReplyTo(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id,asset_id,task_type,state,error_message,updated_at,attempt_id)
		VALUES('task-1','asset-1','asset_download','running',NULL,?,'attempt-1')`, now); err != nil {
		t.Fatal(err)
	}
	client := &Client{DB: db}
	body := protocolv2.SwarmSources{TaskID: "task-1", AttemptID: "attempt-1", AssetID: "asset-1", ManifestID: "manifest-1"}
	wrong, _ := protocolv2.Reply(protocolv2.TypeSwarmSources, "sources-1", "wrong", body)
	if err := client.handleV2Sources(wrong); err == nil {
		t.Fatal("swarm.sources with wrong reply_to accepted")
	}
	correctReply := protocolv2.StableMessageID(protocolv2.TypeSwarmSourcesRequest,
		"task-1", "attempt-1", "asset-1", "manifest-1")
	correct, _ := protocolv2.Reply(protocolv2.TypeSwarmSources, "sources-2", correctReply, body)
	if err := client.handleV2Sources(correct); err != nil {
		t.Fatal(err)
	}

	staleBody := body
	staleBody.AttemptID = "attempt-old"
	push, _ := protocolv2.New(protocolv2.TypeSwarmSources, "push-1", staleBody)
	if err := client.handleV2Sources(push); err != nil {
		t.Fatal(err)
	}
}
