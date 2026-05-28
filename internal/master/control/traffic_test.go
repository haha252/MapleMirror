package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestAcceptTrafficEventAccountsOnceAndStartsTransfer(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedTrafficAuth(t, repo)
	session := Session{ID: "sess-1", NodeID: "node-1"}
	event := protocol.TrafficEvent{
		EventSequence: 1, AuthorizationID: "auth-1", AssetID: "asset-1",
		NodeRequestID: "node-req-1", MasterRequestID: "master-req-1",
		SentBytes: 5, Status: "completed", ReportedAt: time.Now().UTC(),
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 2, event); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptTrafficEvent(context.Background(), session, 3, event); err != nil {
		t.Fatal(err)
	}
	var sent, started int64
	err := repo.DB.QueryRow(`SELECT sent_bytes, transfer_started_count
		FROM daily_project_stats WHERE project_id = 'p1'`).Scan(&sent, &started)
	if err != nil || sent != 5 || started != 1 {
		t.Fatalf("流量幂等入账不符合预期：sent=%d started=%d err=%v", sent, started, err)
	}
}

func seedTrafficAuth(t *testing.T, repo Repository) {
	t.Helper()
	exec := func(query string, args ...any) {
		if _, err := repo.DB.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', ?)`, now)
	exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, ?, 1, ?)`, now, now)
	exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'a.zip', 'amd64', 12,
		'https://example.test/a.zip', 'sha256:aa', 'candidate', ?)`, now)
	exec(`INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'ready', 1, ?, 1, ?, ?)`, now, now, now)
	exec(`INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after,
		status, issued_request_id, created_at)
		VALUES ('cert-1', 'node-1', '1', 'sha256:aa', ?, ?, 'active', 'req', ?)`,
		now, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), now)
	exec(`INSERT INTO node_control_sessions
		(id, node_id, certificate_id, request_id, connected_at, last_message_sequence)
		VALUES ('sess-1', 'node-1', 'cert-1', 'req', ?, 1)`, now)
	exec(`INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES ('auth-1', 'asset-1', 'node-1', '192.0.2.1/32', ?, ?, 12, 4, 'issued', 'master-req-1')`,
		now, now)
	exec(`INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES ('auth-1', '2026-05-29', 12, 12, 0, 'active', ?,
		'ipv4_32', '192.0.2.1/32', 'ipv4_24', '192.0.2.0/24')`, now)
}
