package public

import (
	"context"
	"database/sql"
	"testing"
	"time"

	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/protocol"
)

func TestIssueAuthorizationPrefersHigherDownloadPriority(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPriorityNode(t, db, "node-2", 90, "2026-01-01T00:00:00Z", "online")
	mustExec(t, db, `UPDATE nodes SET download_priority = 10,
		last_heartbeat_at = '2026-01-01T00:00:03Z' WHERE id = 'node-1'`)
	store := Store{DB: db}

	auth := issuePriorityAuth(t, store)
	if auth.Claims.NodeID != "node-2" {
		t.Fatalf("authorization node=%s want node-2", auth.Claims.NodeID)
	}
}

func TestIssueAuthorizationUsesPressureAwareScore(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPriorityNode(t, db, "node-2", 60, "2026-01-01T00:00:00Z", "online")
	mustExec(t, db, `UPDATE nodes SET download_priority = 100,
		last_heartbeat_at = '2026-01-01T00:00:03Z' WHERE id = 'node-1'`)
	runtime := mastercontrol.NewRuntimeStore()
	acceptRoutingPressure(t, db, runtime, "node-1", 1, 100, 150, 20)
	acceptRoutingPressure(t, db, runtime, "node-2", 1, 100, 0, 0)
	store := Store{DB: db, Runtime: runtime}

	auth := issuePriorityAuth(t, store)
	if auth.Claims.NodeID != "node-2" {
		t.Fatalf("authorization node=%s want node-2", auth.Claims.NodeID)
	}
}

func TestIssueAuthorizationSkipsUnavailableHighPriorityNode(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPriorityNode(t, db, "node-2", 100, "2026-01-01T00:00:03Z", "offline")
	mustExec(t, db, `UPDATE nodes SET download_priority = 50 WHERE id = 'node-1'`)
	store := Store{DB: db}

	auth := issuePriorityAuth(t, store)
	if auth.Claims.NodeID != "node-1" {
		t.Fatalf("authorization node=%s want node-1", auth.Claims.NodeID)
	}
}

func TestIssueAuthorizationDoesNotPenalizeMissingPressure(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPriorityNode(t, db, "node-2", 60, "2026-01-01T00:00:03Z", "online")
	mustExec(t, db, `UPDATE nodes SET download_priority = 100,
		last_heartbeat_at = '2026-01-01T00:00:00Z' WHERE id = 'node-1'`)
	store := Store{DB: db, Runtime: mastercontrol.NewRuntimeStore()}

	auth := issuePriorityAuth(t, store)
	if auth.Claims.NodeID != "node-1" {
		t.Fatalf("authorization node=%s want node-1", auth.Claims.NodeID)
	}
}

func TestIssueAuthorizationFallsBackToHeartbeatWithinSamePriority(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPriorityNode(t, db, "node-2", 50, "2026-01-01T00:00:03Z", "online")
	mustExec(t, db, `UPDATE nodes SET download_priority = 50,
		last_heartbeat_at = '2026-01-01T00:00:00Z' WHERE id = 'node-1'`)
	store := Store{DB: db}

	auth := issuePriorityAuth(t, store)
	if auth.Claims.NodeID != "node-2" {
		t.Fatalf("authorization node=%s want node-2", auth.Claims.NodeID)
	}
}

func issuePriorityAuth(t *testing.T, store Store) IssuedAuthorization {
	t.Helper()
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func seedPriorityNode(t *testing.T, db *sql.DB, nodeID string, priority int, heartbeat, state string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, download_priority,
		last_heartbeat_at, routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('`+nodeID+`', '`+nodeID+`', '`+state+`', 1, ?, ?, 1,
		'https://`+nodeID+`.example.com', '2026-01-01T00:00:00Z',
		'2026-01-01T00:00:02Z')`, priority, heartbeat); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('`+nodeID+`', 'asset-1', 'sha256:aa', 12,
		'2026-01-01T00:00:01Z', 'verified')`)
	mustExec(t, db, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('`+nodeID+`', 'asset-1', 'required', 'now')`)
}

func acceptRoutingPressure(t *testing.T, db *sql.DB, runtime *mastercontrol.RuntimeStore,
	nodeID string, seq uint64, target, actual, active int64) {
	t.Helper()
	repo := mastercontrol.Repository{DB: db, Runtime: runtime}
	session := mastercontrol.Session{ID: "session-" + nodeID, NodeID: nodeID, RequestID: "req-" + nodeID}
	runtime.StartSession(session)
	_, err := repo.AcceptPressureReport(context.Background(), session, seq, protocol.PressureReport{
		SampleWindowSeconds: 10,
		TargetBandwidthBPS:  target,
		ActualBandwidthBPS:  actual,
		ActiveDownloads:     active,
		FreeBytes:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
}
