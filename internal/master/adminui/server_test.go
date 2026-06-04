package adminui

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	server, db := newTestServer(t)
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
	var sessions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_web_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("sessions = %d, want 1", sessions)
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
	if blocks != 1 {
		t.Fatalf("blocks = %d, want 1", blocks)
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
	enabled := true
	usersPath := filepath.Join(dir, "users.yaml")
	server, err := New(config.Administration{
		AllowedCIDRs: []string{"127.0.0.0/8"},
		Web: config.AdminWeb{
			Enabled: &enabled, UsersFile: usersPath,
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

func TestSaveProjectsRequiresMTLSOutsideLoopback(t *testing.T) {
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

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(dir, "master.db"), BusyTimeout: "5s", WAL: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	usersPath := filepath.Join(dir, "users.yaml")
	hash := testPasswordHash("correct-password")
	body := "users:\n  - username: admin\n    password_hash: \"" + hash + "\"\n"
	if err := os.WriteFile(usersPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled := true
	server, err := New(config.Administration{
		AllowedCIDRs: []string{"127.0.0.0/8"},
		Web: config.AdminWeb{
			Enabled: &enabled, UsersFile: usersPath,
			SessionSecretFile: filepath.Join(dir, "session.key"),
			SessionTTL:        "12h", LoginFailureWindow: "24h",
			LoginFailureLimit: 3, LoginBanDuration: "168h",
		},
	}, mastercontrol.Repository{DB: db}, mirrorsync.Store{DB: db}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return server, db
}

func loginRequest(username, password string) *http.Request {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func sessionFrom(cookies []*http.Cookie) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	return nil
}

func testPasswordHash(password string) string {
	salt := []byte("0123456789abcdef")
	key := pbkdf2SHA256([]byte(password), salt, 100000, 32)
	return "pbkdf2-sha256$100000$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key)
}

func boolPtr(value bool) *bool {
	return &value
}
