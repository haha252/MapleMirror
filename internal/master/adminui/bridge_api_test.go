package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSyncTasksAPIFiltersByNodeAndLimitsFields(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, error_message, attempts)
		VALUES ('task-1', 'node-1', 'asset_download', 'failed', 'req-1', 'now', '失败摘要', 2)`)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/sync/tasks?node_id=node-1", nil)
	rec := httptest.NewRecorder()
	server.syncTasksAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tasks      []map[string]any `json:"tasks"`
		Pagination pagination       `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 1 || body.Tasks[0]["task_id"] != "task-1" {
		t.Fatalf("unexpected tasks: %+v", body)
	}
	if body.Pagination.Total != 1 {
		t.Fatalf("pagination total = %d, want 1", body.Pagination.Total)
	}
	if _, ok := body.Tasks[0]["download_url"]; ok {
		t.Fatal("task list must not expose download URLs")
	}
}

func TestSyncTasksAPIPaginatesByNode(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, attempts)
		VALUES ('task-old', 'node-1', 'asset_download', 'pending', 'req-1', '2026-06-01T00:00:00Z', 0)`)
	mustExecAdminUI(t, db, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, attempts)
		VALUES ('task-new', 'node-1', 'asset_download', 'pending', 'req-2', '2026-06-02T00:00:00Z', 0)`)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/sync/tasks?node_id=node-1&page=2&page_size=1", nil)
	rec := httptest.NewRecorder()
	server.syncTasksAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tasks      []map[string]any `json:"tasks"`
		Pagination pagination       `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Pagination.Page != 2 || body.Pagination.PageSize != 1 || body.Pagination.Total != 2 {
		t.Fatalf("pagination = %+v", body.Pagination)
	}
	if len(body.Tasks) != 1 || body.Tasks[0]["task_id"] != "task-old" {
		t.Fatalf("unexpected page rows: %+v", body.Tasks)
	}
}

func TestStatsAuthorizationAndTrafficBridge(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		digest_sha256, source_url, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'demo.zip', '', 64, 'sha',
		'https://example.test/demo.zip', 'active', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('2026-06-06', 'p1', 2, 1, 128)`)
	mustExecAdminUI(t, db, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id, first_transfer_at)
		VALUES ('auth-1', 'asset-1', 'node-1', 'client-*', 'now', 'later',
		1000, 1, 'issued', 'req-auth', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes,
		settled_bytes, status, created_at, address_scope_kind, address_scope_key,
		network_scope_kind, network_scope_key)
		VALUES ('auth-1', '2026-06-06', 1000, 1000, 64, 'active', 'now',
		'ipv4_32', '192.0.2.1/32', 'ipv4_24', '192.0.2.0/24')`)
	mustExecAdminUI(t, db, `INSERT INTO traffic_event_dedupe
		(node_id, event_sequence, authorization_id, event_hash, accounted_at)
		VALUES ('node-1', 1, 'auth-1', 'hash-1', 'now')`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/stats/projects?day=2026-06-06", nil)
	server.projectStatsAPI(rec, req)
	if rec.Code != http.StatusOK || !containsBody(rec, "p1") {
		t.Fatalf("project stats failed status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/authorizations/auth-1", nil)
	server.authorizationAPI(rec, req)
	if rec.Code != http.StatusOK || !containsBody(rec, "client-*") {
		t.Fatalf("authorization failed status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/traffic/events?authorization_id=auth-1", nil)
	server.trafficEventsAPI(rec, req)
	if rec.Code != http.StatusOK || !containsBody(rec, "hash-1") {
		t.Fatalf("traffic failed status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrafficEventsAPIPaginates(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		digest_sha256, source_url, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'demo.zip', '', 64, 'sha',
		'https://example.test/demo.zip', 'active', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at,
		max_bytes, range_limit, status, request_id)
		VALUES ('auth-1', 'asset-1', 'node-1', 'client-*', 'now', 'later',
		1000, 1, 'issued', 'req-auth')`)
	mustExecAdminUI(t, db, `INSERT INTO traffic_event_dedupe
		(node_id, event_sequence, authorization_id, event_hash, accounted_at)
		VALUES ('node-1', 1, 'auth-1', 'hash-1', '2026-06-01T00:00:00Z')`)
	mustExecAdminUI(t, db, `INSERT INTO traffic_event_dedupe
		(node_id, event_sequence, authorization_id, event_hash, accounted_at)
		VALUES ('node-1', 2, 'auth-1', 'hash-2', '2026-06-02T00:00:00Z')`)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/traffic/events?authorization_id=auth-1&page=2&page_size=1", nil)
	rec := httptest.NewRecorder()
	server.trafficEventsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events     []map[string]any `json:"events"`
		Pagination pagination       `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Pagination.Page != 2 || body.Pagination.PageSize != 1 || body.Pagination.Total != 2 {
		t.Fatalf("pagination = %+v", body.Pagination)
	}
	if len(body.Events) != 1 || body.Events[0]["event_hash"] != "hash-1" {
		t.Fatalf("unexpected traffic page: %+v", body.Events)
	}
}

func TestAuditEventsAPIDoesNotExposeAdminIdentity(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO admin_audit_events
		(id, operation, target_type, target_id, result, request_id, admin_identity,
		details_summary, created_at)
		VALUES ('audit-1', 'node.disable', 'node', 'node-1', 'success', 'req-1',
		'admin-cert-secret', '节点已禁用', 'now')`)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/security/audit-events", nil)
	rec := httptest.NewRecorder()
	server.auditEventsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d body=%s", rec.Code, rec.Body.String())
	}
	if containsBody(rec, "admin-cert-secret") {
		t.Fatalf("audit response leaked admin identity: %s", rec.Body.String())
	}
}

func containsBody(rec *httptest.ResponseRecorder, text string) bool {
	return json.Valid(rec.Body.Bytes()) && strings.Contains(rec.Body.String(), text)
}
