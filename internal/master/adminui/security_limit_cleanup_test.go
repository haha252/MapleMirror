package adminui

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeleteClientBlockClearsOnlyIPv4HostLimitState(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/client/192.0.2.10/32", nil)
	seedClientBlockLimitState(t, db, "192.0.2.10/32", "ipv4_32", "192.0.2.10/32")
	seedLimitState(t, db, "ipv4_24", "192.0.2.0/24")

	if err := server.deleteBlock(req, "client", "192.0.2.10/32"); err != nil {
		t.Fatal(err)
	}

	assertTableCountWhere(t, db, "client_blocks", "client_prefix_key = '192.0.2.10/32'", 0)
	assertLimitStateCleared(t, db, "ipv4_32", "192.0.2.10/32")
	assertLimitStatePresent(t, db, "ipv4_24", "192.0.2.0/24")
	assertReservationStatus(t, db, authIDForScope("ipv4_32", "192.0.2.10/32"), "released")
	assertReservationStatus(t, db, authIDForScope("ipv4_24", "192.0.2.0/24"), "active")
}

func TestDeleteClientBlockClearsOnlyIPv4NetworkLimitState(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/client/192.0.2.0/24", nil)
	seedClientBlockLimitState(t, db, "192.0.2.0/24", "ipv4_24", "192.0.2.0/24")
	seedLimitState(t, db, "ipv4_32", "192.0.2.10/32")

	if err := server.deleteBlock(req, "client", "192.0.2.0/24"); err != nil {
		t.Fatal(err)
	}

	assertLimitStateCleared(t, db, "ipv4_24", "192.0.2.0/24")
	assertLimitStatePresent(t, db, "ipv4_32", "192.0.2.10/32")
	assertReservationStatus(t, db, authIDForScope("ipv4_24", "192.0.2.0/24"), "released")
	assertReservationStatus(t, db, authIDForScope("ipv4_32", "192.0.2.10/32"), "active")
}

func TestDeleteClientBlockClearsOnlyIPv6HostLimitState(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/client/2001:db8::1/128", nil)
	seedClientBlockLimitState(t, db, "2001:db8::1/128", "ipv6_128", "2001:db8::1/128")
	seedLimitState(t, db, "ipv6_128", "2001:db8::2/128")

	if err := server.deleteBlock(req, "client", "2001:db8::1/128"); err != nil {
		t.Fatal(err)
	}

	assertLimitStateCleared(t, db, "ipv6_128", "2001:db8::1/128")
	assertLimitStatePresent(t, db, "ipv6_128", "2001:db8::2/128")
	assertReservationStatus(t, db, authIDForScope("ipv6_128", "2001:db8::1/128"), "released")
}

func TestDeleteAdminBlockDoesNotClearClientLimitState(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/admin/key", nil)
	key := server.store.ipKey("192.0.2.10")
	mustExecAdminUI(t, db, `INSERT INTO admin_ip_blocks
		(ip_key, masked_ip, display_ip, reason, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES (?, '192.0.2.*', '192.0.2.10', '管理封禁', ?, ?, 0, ?, ?)`,
		key, nowText(), futureText(), nowText(), nowText())
	seedLimitState(t, db, "ipv4_32", "192.0.2.10/32")

	if err := server.deleteBlock(req, "admin", key); err != nil {
		t.Fatal(err)
	}

	assertTableCountWhere(t, db, "admin_ip_blocks", "ip_key = '"+key+"'", 0)
	assertLimitStatePresent(t, db, "ipv4_32", "192.0.2.10/32")
	assertReservationStatus(t, db, authIDForScope("ipv4_32", "192.0.2.10/32"), "active")
}

func seedClientBlockLimitState(t *testing.T, db *sql.DB, blockKey, scopeKind, scopeKey string) {
	t.Helper()
	mustExecAdminUI(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES (?, '自动封禁', 'local_auto_ban', ?, ?, 0, ?, ?)`,
		blockKey, nowText(), futureText(), nowText(), nowText())
	seedLimitState(t, db, scopeKind, scopeKey)
}

func seedLimitState(t *testing.T, db *sql.DB, scopeKind, scopeKey string) {
	t.Helper()
	day := statDay(time.Now().UTC(), nil)
	seedLimitForeignKeys(t, db)
	mustExecAdminUI(t, db, `INSERT INTO quota_buckets
		(scope_kind, scope_key, tokens_microunits, updated_at)
		VALUES (?, ?, 0, ?)`, scopeKind, scopeKey, nowText())
	mustExecAdminUI(t, db, `INSERT INTO daily_traffic_stats
		(stat_day, scope_kind, scope_key, sent_bytes, updated_at)
		VALUES (?, ?, ?, 1234, ?)`, day, scopeKind, scopeKey, nowText())
	authID := authIDForScope(scopeKind, scopeKey)
	mustExecAdminUI(t, db, `INSERT OR IGNORE INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES (?, 'asset-1', 'node-1', ?, ?, ?, 100, 4, 'issued', ?)`,
		authID, scopeKey, nowText(), futureText(), "req-"+scopeKind)
	mustExecAdminUI(t, db, `INSERT OR REPLACE INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES (?, ?, 100, 100, 0, 'active', ?, ?, ?, ?, ?)`,
		authID, day, nowText(), scopeKind, scopeKey, scopeKind, scopeKey)
}

func seedLimitForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecAdminUI(t, db, `INSERT OR IGNORE INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('project-1', 'Project 1', 'owner/project-1', 1, 3, 0, 1, 'hash', ?)`,
		nowText())
	mustExecAdminUI(t, db, `INSERT OR IGNORE INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at,
		selected, created_at)
		VALUES ('release-1', 'project-1', 1, 'v1.0.0', 0, ?, 1, ?)`,
		nowText(), nowText())
	mustExecAdminUI(t, db, `INSERT OR IGNORE INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'release-1', 1, 'asset.tar.gz', 'amd64', 100,
		'https://example.com/asset.tar.gz', 'sha256', 'candidate', ?)`, nowText())
	mustExecAdminUI(t, db, `INSERT OR IGNORE INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', 'Node 1', 'online', 0, 1, ?, ?)`, nowText(), nowText())
}

func assertLimitStateCleared(t *testing.T, db *sql.DB, scopeKind, scopeKey string) {
	t.Helper()
	assertTableCountWhere(t, db, "quota_buckets",
		"scope_kind = '"+scopeKind+"' AND scope_key = '"+scopeKey+"'", 0)
	assertTableCountWhere(t, db, "daily_traffic_stats",
		"scope_kind = '"+scopeKind+"' AND scope_key = '"+scopeKey+"'", 0)
}

func assertLimitStatePresent(t *testing.T, db *sql.DB, scopeKind, scopeKey string) {
	t.Helper()
	assertTableCountWhere(t, db, "quota_buckets",
		"scope_kind = '"+scopeKind+"' AND scope_key = '"+scopeKey+"'", 1)
	assertTableCountWhere(t, db, "daily_traffic_stats",
		"scope_kind = '"+scopeKind+"' AND scope_key = '"+scopeKey+"'", 1)
}

func assertReservationStatus(t *testing.T, db *sql.DB, authID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM traffic_reservations
		WHERE authorization_id = ?`, authID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("reservation %s status = %s, want %s", authID, got, want)
	}
}

func assertTableCountWhere(t *testing.T, db *sql.DB, table, where string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE ` + where).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s where %s count = %d, want %d", table, where, got, want)
	}
}

func authIDForScope(scopeKind, scopeKey string) string {
	replacer := strings.NewReplacer(":", "-", ".", "-", "/", "-")
	return "auth-" + scopeKind + "-" + replacer.Replace(scopeKey)
}

func futureText() string {
	return time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
}
