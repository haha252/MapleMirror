package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestAcceptAuthorizationStatusEventUpdatesAuthorization(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()
	seedAuthorizationStatusAuth(t, repo, "node-1")
	session := Session{ID: "sess-1", NodeID: "node-1"}
	repo.runtime().StartSession(session)
	result, err := repo.AcceptAuthorizationStatusEvent(context.Background(), session, 2,
		protocol.AuthorizationStatusEvent{AuthorizationID: "auth-1", AssetID: "asset-1",
			Status: "expired_idle", Reason: "expired_idle", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if result.AcceptedSequence != 2 {
		t.Fatalf("accepted sequence=%d", result.AcceptedSequence)
	}
	var status string
	if err := repo.DB.QueryRow(`SELECT status FROM download_authorizations
		WHERE id = 'auth-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "expired_idle" {
		t.Fatalf("status=%q", status)
	}
}

func TestAcceptAuthorizationStatusEventRejectsWrongNode(t *testing.T) {
	repo, cleanup := testRepo(t)
	defer cleanup()
	seedAuthorizationStatusAuth(t, repo, "node-1")
	repo.runtime().StartSession(Session{ID: "sess-1", NodeID: "node-2"})
	_, err := repo.AcceptAuthorizationStatusEvent(context.Background(),
		Session{ID: "sess-1", NodeID: "node-2"}, 2,
		protocol.AuthorizationStatusEvent{AuthorizationID: "auth-1", AssetID: "asset-1",
			Status: "expired_idle", OccurredAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("wrong node should be rejected")
	}
}

func seedAuthorizationStatusAuth(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := repo.DB.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		 download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, ?, 1, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		 source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'a.zip', 'amd64', 10,
		 'https://example.test/a.zip', 'sha256:aa', 'candidate', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.Exec(`INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES (?, '节点一', 'syncing', 0, 1, ?, ?)`, nodeID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.Exec(`INSERT INTO node_control_sessions
		(id, node_id, request_id, connected_at, last_message_sequence)
		VALUES ('sess-1', ?, 'req-1', ?, 0)`, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.Exec(`INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		 max_bytes, range_limit, status, request_id)
		VALUES ('auth-1', 'asset-1', ?, '192.0.2.1/32', ?, ?, 10, 2, 'issued', 'req-1')`,
		nodeID, now, now); err != nil {
		t.Fatal(err)
	}
}
