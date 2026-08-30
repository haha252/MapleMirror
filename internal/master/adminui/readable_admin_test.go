package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncTasksAPIIncludesHumanReadableAssetMetadata(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '示例项目', 'owner/repo', 1, 3, 0, 1, 'hash', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 11, 'v2.0.0', 0, 'now', 1, 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, system, size_bytes,
		digest_sha256, source_url, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 22, 'demo-windows-amd64.zip', 'amd64', 'windows', 4096,
		'sha', 'https://example.test/private-source', 'active', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '香港节点', 'online', 0, 1, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, attempts)
		VALUES ('task-1', 'node-1', 'asset_download', 'asset-1', 'running', 'req-1', 'now', 1)`)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/sync/tasks?node_id=node-1", nil)
	rec := httptest.NewRecorder()
	server.syncTasksAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 1 {
		t.Fatalf("tasks=%+v", body.Tasks)
	}
	got := body.Tasks[0]
	checks := map[string]any{
		"file_name":    "demo-windows-amd64.zip",
		"project_name": "示例项目",
		"project_id":   "p1",
		"version":      "v2.0.0",
		"architecture": "amd64",
		"system":       "windows",
		"node_name":    "香港节点",
	}
	for key, want := range checks {
		if got[key] != want {
			t.Fatalf("%s=%v want %v", key, got[key], want)
		}
	}
	if got["size_bytes"] != float64(4096) {
		t.Fatalf("size_bytes=%v", got["size_bytes"])
	}
	if _, ok := got["source_url"]; ok {
		t.Fatal("task list must not expose source_url")
	}
	if _, ok := got["download_url"]; ok {
		t.Fatal("task list must not expose download_url")
	}
}

func TestSecuritySummarySeparatesAttentionFromNormalBlocks(t *testing.T) {
	server, db := newTestServer(t)
	now := time.Now().UTC()
	activeUntil := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	recent := now.Add(-5 * time.Minute).Format(time.RFC3339Nano)
	mustExecAdminUI(t, db, `INSERT INTO admin_login_failures
		(ip_key, masked_ip, failed_count, window_started_at, last_failed_at, updated_at)
		VALUES ('login-key', '192.0.2.*', 2, ?, ?, ?)`, recent, recent, recent)
	mustExecAdminUI(t, db, `INSERT INTO admin_ip_blocks
		(ip_key, masked_ip, display_ip, reason, blocked_at, expires_at,
		attempts_after_block, last_attempt_at, updated_at)
		VALUES ('admin-key', '198.51.100.*', '198.51.100.20', 'manual', ?, ?, 0, ?, ?)`,
		recent, activeUntil, recent, recent)
	mustExecAdminUI(t, db, `INSERT INTO client_blocks
		(client_prefix_key, reason, source, blocked_at, expires_at, attempts_after_block,
		last_attempt_at, updated_at, escalation_level, punishment_active)
		VALUES ('203.0.113.0/24', 'abuse', 'automatic', ?, ?, 20, ?, ?, 2, 1)`,
		recent, activeUntil, recent, recent)
	mustExecAdminUI(t, db, `INSERT INTO admin_web_sessions
		(id, username, ip_key, created_at, expires_at, last_seen_at)
		VALUES ('session-1', 'admin', 'ip-key', ?, ?, ?)`, recent, activeUntil, recent)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/security/summary", nil)
	rec := httptest.NewRecorder()
	server.securitySummaryAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AttentionCount int              `json:"attention_count"`
		ActiveBlocks   int              `json:"active_blocks"`
		Punishment     int              `json:"punishment_active"`
		Sessions       int              `json:"active_sessions"`
		Warnings       []map[string]any `json:"login_warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AttentionCount != 2 {
		t.Fatalf("attention_count=%d want 2", body.AttentionCount)
	}
	if body.ActiveBlocks != 2 {
		t.Fatalf("active_blocks=%d want 2", body.ActiveBlocks)
	}
	if body.Punishment != 1 || body.Sessions != 1 {
		t.Fatalf("punishment=%d sessions=%d", body.Punishment, body.Sessions)
	}
	if len(body.Warnings) != 1 || body.Warnings[0]["masked_ip"] != "192.0.2.*" {
		t.Fatalf("warnings=%+v", body.Warnings)
	}
}
