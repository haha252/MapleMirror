package admin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func TestProjectResetAllowsLoopbackWithoutMTLS(t *testing.T) {
	server, _ := projectResetServer(t)
	req := adminRequest("POST", "/api/admin/v1/projects/p1/reset", `{"reason":"fix"}`)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectResetReturnsNotFoundForUnknownProject(t *testing.T) {
	server, _ := projectResetServer(t)
	req := adminRequestWithTLS("POST", "/api/admin/v1/projects/p2/reset", `{"reason":"fix"}`)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectResetClearsDataAndTriggersScan(t *testing.T) {
	server, syncStub := projectResetServer(t)
	req := adminRequestWithTLS("POST", "/api/admin/v1/projects/p1/reset", `{"reason":"修正了错误的配置"}`)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data := body.Data.(map[string]any)
	if data["scan_id"] != "scan-new" {
		t.Fatalf("unexpected scan_id: %#v", data)
	}
	if syncStub.projectID != "p1" {
		t.Fatalf("triggered wrong project: %#v", syncStub.projectID)
	}
	assertCountAdmin(t, server.Repo.DB, "projects", 1)
	assertCountAdmin(t, server.Repo.DB, "releases", 0)
	assertCountAdmin(t, server.Repo.DB, "assets", 0)
	assertCountAdmin(t, server.Repo.DB, "target_inventory", 0)
	assertCountAdmin(t, server.Repo.DB, "node_tasks", 0)
	assertCountAdmin(t, server.Repo.DB, "node_inventory", 0)
	assertCountAdmin(t, server.Repo.DB, "sync_scans", 0)
	assertCountAdmin(t, server.Repo.DB, "project_scan_state", 0)
	assertCountAdmin(t, server.Repo.DB, "daily_project_stats", 0)
	assertCountAdmin(t, server.Repo.DB, "daily_asset_stats", 0)
	assertCountAdmin(t, server.Repo.DB, "download_authorizations", 0)
	assertCountAdmin(t, server.Repo.DB, "traffic_reservations", 0)
	assertCountAdmin(t, server.Repo.DB, "traffic_events", 0)
	assertCountAdmin(t, server.Repo.DB, "challenges", 0)
	var audits int
	if err := server.Repo.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE operation = 'project.reset' AND target_id = 'p1'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("unexpected audit rows: %d", audits)
	}
}

func projectResetServer(t *testing.T) (Server, *stubSync) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seedProjectResetData(t, db, now)
	projects := staticProjects{projects: config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "p1", Repository: "owner/one", Enabled: true,
		RetainVersions: 1,
	}}}}
	syncStub := &stubSync{scanID: "scan-new"}
	server := Server{
		Auth:      testAuth(t),
		Repo:      control.Repository{DB: db},
		Sync:      syncStub,
		SyncStore: mirrorsync.Store{DB: db},
		Projects:  projects,
	}
	return server, syncStub
}

func seedProjectResetData(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, now string) {
	t.Helper()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', 'p1', 'owner/one', 1, 1, 0, 1, 'hash', ?)`, now)
	exec(`INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-1', 'node-1', 'syncing', 0, 0, ?, ?)`, now, now)
	exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('p1:1', 'p1', 1, 'v1', 0, ?, 1, ?)`, now, now)
	exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes, source_url,
		digest_sha256, service_state, created_at)
		VALUES ('p1:1:1', 'p1:1', 1, 'app.zip', '', 10, 'https://example.invalid/a',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'candidate', ?)`, now)
	exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'p1:1:1', 'required', ?)`, now)
	exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-1', 'p1:1:1', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 10, ?, 'verified')`, now)
	exec(`INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, completed_at,
		error_message, attempts, updated_at, retry_after)
		VALUES ('task-1', 'node-1', 'asset_download', 'p1:1:1', 'pending', 'req-1', ?, NULL,
		NULL, 0, ?, NULL)`, now, now)
	exec(`INSERT INTO sync_scans
		(id, project_id, state, selected_releases, accepted_assets, rejected_assets,
		error_message, request_id, started_at, completed_at, next_allowed_scan_at)
		VALUES ('scan-1', 'p1', 'succeeded', 1, 1, 0, NULL, 'req-scan', ?, ?, NULL)`, now, now)
	exec(`INSERT INTO project_scan_state
		(project_id, enabled, config_hash, last_scan_started_at, last_scan_completed_at,
		last_scan_id, last_scan_state, next_scan_at, last_error_message, updated_at)
		VALUES ('p1', 1, 'hash', ?, ?, 'scan-1', 'succeeded', ?, NULL, ?)`, now, now, now, now)
	exec(`INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('2026-06-03', 'p1', 2, 1, 100)`)
	exec(`INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('2026-06-03', 'p1:1:1', 2, 1, 100, ?)`, now)
	exec(`INSERT INTO download_authorizations
		(id, asset_id, node_id, client_prefix_key, issued_at, expires_at, max_bytes, range_limit, status, request_id, first_transfer_at)
		VALUES ('auth-1', 'p1:1:1', 'node-1', 'prefix', ?, ?, 10, 1, 'active', 'req-auth', ?)`, now, now, now)
	exec(`INSERT INTO traffic_reservations
		(authorization_id, scope_day, address_reserved_bytes, network_reserved_bytes, settled_bytes, status, created_at)
		VALUES ('auth-1', '2026-06-03', 10, 10, 0, 'active', ?)`, now)
	exec(`INSERT INTO traffic_events
		(node_id, event_sequence, authorization_id, node_request_id, master_request_id, sent_bytes, reported_at, accounted_at)
		VALUES ('node-1', 1, 'auth-1', 'node-req-1', 'master-req-1', 10, ?, ?)`, now, now)
	exec(`INSERT INTO challenges
		(id, kind, asset_id, client_prefix_key, nonce_hash, difficulty, expires_at, consumed_at, request_id)
		VALUES ('challenge-1', 'altcha', 'p1:1:1', 'prefix', 'nonce', 1, ?, NULL, 'req-challenge')`, now)
	exec(`INSERT INTO daily_traffic_stats
		(stat_day, scope_kind, scope_key, sent_bytes, updated_at)
		VALUES ('2026-06-03', 'ipv4_32', '192.0.2.1/32', 777, ?)`, now)
}

type staticProjects struct {
	projects config.Projects
}

func (s staticProjects) Load() (config.Projects, error) { return s.projects, nil }

func (s staticProjects) Current() config.Projects { return s.projects }

type stubSync struct {
	projectID string
	scanID    string
	err       error
}

func (s *stubSync) Trigger(_ context.Context, projectID, _ string) (string, error) {
	s.projectID = projectID
	if s.scanID == "" {
		s.scanID = "scan-new"
	}
	return s.scanID, s.err
}

func adminRequestWithTLS(method, target, body string) *http.Request {
	req := adminRequest(method, target, body)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
		Subject:     pkix.Name{CommonName: "admin-cn"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}}}
	return req
}

func assertCountAdmin(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s got=%d want=%d", table, got, want)
	}
}
