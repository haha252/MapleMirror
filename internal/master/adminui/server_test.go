package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func TestLoginCreatesSecureSession(t *testing.T) {
	server, _ := newTestServer(t)
	req := loginRequest("admin", "correct-password")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d", rec.Code)
	}
	cookie := sessionFrom(rec.Result().Cookies())
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("expected secure session cookie, got %#v", cookie)
	}
	check := httptest.NewRecorder()
	next := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	next.RemoteAddr = "127.0.0.1:55000"
	next.AddCookie(cookie)
	server.Handler().ServeHTTP(check, next)
	if check.Code != http.StatusOK {
		t.Fatalf("session should authorize overview, code=%d body=%s", check.Code, check.Body.String())
	}
}

func TestLoginBlocksIPAfterThreeFailures(t *testing.T) {
	server, db := newTestServer(t)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, loginRequest("admin", "wrong-password"))
		if rec.Code != http.StatusOK {
			t.Fatalf("failure status = %d", rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, loginRequest("admin", "correct-password"))
	if sessionFrom(rec.Result().Cookies()) != nil {
		t.Fatal("blocked login should not create a session")
	}
	var blocks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_ip_blocks`).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if blocks != 0 {
		t.Fatalf("automatic login blocks should stay in memory, db blocks=%d", blocks)
	}
}

func TestSaveProjectsWritesFileAndSyncsState(t *testing.T) {
	server, db := newTestServer(t)
	projectsPath := filepath.Join(t.TempDir(), "projects.yaml")
	initial := config.Projects{Projects: []config.Project{{
		ID: "old", Name: "旧项目", Repository: "owner/old",
		Enabled: false, RetainVersions: 1, DownloadMultiplier: 1,
	}}}
	if err := writeProjectsFile(projectsPath, initial); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader(projectsPath, initial)
	body, _ := json.Marshal(config.Projects{Projects: []config.Project{{
		ID: "demo", Name: "演示项目", Repository: "owner/demo",
		Enabled: true, RetainVersions: 2, DownloadMultiplier: 1,
		AssetInclude: config.AssetRules{{Pattern: "*.zip", Type: "glob"}},
	}}})
	req := httptest.NewRequest(http.MethodPut, "/admin/api/projects", strings.NewReader(string(body)))
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.saveProjects(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", rec.Code, rec.Body.String())
	}
	var enabled int
	err := db.QueryRow(`SELECT enabled FROM project_scan_state WHERE project_id = 'demo'`).Scan(&enabled)
	if err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("enabled = %d, want 1", enabled)
	}
	loaded, err := config.LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects[0].ID != "demo" {
		t.Fatalf("saved project = %s", loaded.Projects[0].ID)
	}
}

func TestBootstrapPasswordEnvCreatesUsersFile(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(dir, "master.db"), BusyTimeout: "5s", WAL: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	t.Setenv("MIRROR_ADMIN_WEB_PASSWORD", "created-password")
	usersPath := filepath.Join(dir, "users.yaml")
	server, err := New(config.Administration{
		Web: config.AdminWeb{
			UsersFile:            usersPath,
			BootstrapPasswordEnv: "MIRROR_ADMIN_WEB_PASSWORD",
			SessionSecretFile:    filepath.Join(dir, "session.key"),
			SessionTTL:           "12h", LoginFailureWindow: "24h",
			LoginFailureLimit: 3, LoginBanDuration: "168h",
		},
	}, mastercontrol.Repository{DB: db}, mirrorsync.Store{DB: db}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, loginRequest("admin", "created-password"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d", rec.Code)
	}
	if _, err := os.Stat(usersPath); err != nil {
		t.Fatal(err)
	}
}

func TestSaveProjectsRequiresLoginSession(t *testing.T) {
	server, _ := newTestServer(t)
	projectsPath := filepath.Join(t.TempDir(), "projects.yaml")
	initial := config.Projects{Projects: []config.Project{{
		ID: "demo", Name: "演示项目", Repository: "owner/demo",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	}}}
	if err := writeProjectsFile(projectsPath, initial); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader(projectsPath, initial)
	body, _ := json.Marshal(initial)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/projects", strings.NewReader(string(body)))
	req.RemoteAddr = "192.0.2.10:55000"
	rec := httptest.NewRecorder()
	server.saveProjects(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("save status = %d, want 403", rec.Code)
	}
}

func TestSaveProjectsRequiresOwnerRole(t *testing.T) {
	server, _ := newTestServer(t)
	server.users["viewer"] = userRecord{
		Username: "viewer", PasswordHash: testPasswordHash("viewer-password"),
		Role: "viewer",
	}
	projectsPath := filepath.Join(t.TempDir(), "projects.yaml")
	initial := config.Projects{Projects: []config.Project{{
		ID: "demo", Name: "演示项目", Repository: "owner/demo",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	}}}
	if err := writeProjectsFile(projectsPath, initial); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader(projectsPath, initial)
	body, _ := json.Marshal(initial)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/projects", strings.NewReader(string(body)))
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUsername(req, "viewer")
	rec := httptest.NewRecorder()
	server.saveProjects(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer save status = %d body=%s", rec.Code, rec.Body.String())
	}
	loaded, err := config.LoadProjects(projectsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects[0].ID != "demo" {
		t.Fatalf("viewer must not rewrite projects file, got %s", loaded.Projects[0].ID)
	}
}

func TestSecurityBlockDeleteReportsMissingRecord(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO admin_ip_blocks
		(ip_key, masked_ip, reason, blocked_at, expires_at, attempts_after_block,
		last_attempt_at, updated_at)
		VALUES ('ip-key', '192.0.2.*', 'too_many_failures', 'now', 'tomorrow', 0, 'now', 'now')`)

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/admin/ip-key", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.securityBlockActionAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/admin/api/security/blocks/admin/missing", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec = httptest.NewRecorder()
	server.securityBlockActionAPI(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func boolPtr(value bool) *bool {
	return &value
}
