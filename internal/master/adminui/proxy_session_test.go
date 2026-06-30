package adminui

import (
	"database/sql"
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

func TestAdminShellRequiresLoginThenRendersAfterSession(t *testing.T) {
	server, _ := newProxyTestServer(t, nil, config.AdminWeb{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("shell redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}

	cookie := loginCookie(t, server, "127.0.0.1:55000", "")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req.AddCookie(cookie)
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "管理面板") {
		t.Fatalf("shell status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminWebLoginUsesPanelSession(t *testing.T) {
	server, _ := newProxyTestServer(t, nil, config.AdminWeb{})
	req := loginForm("admin", "correct-password")
	req.RemoteAddr = "198.51.100.10:55000"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("web login status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTrustedProxyIPDrivesLoginBlockAndSession(t *testing.T) {
	server, _ := newProxyTestServer(t, []string{"127.0.0.0/8"}, config.AdminWeb{})
	cookie := loginCookie(t, server, "127.0.0.1:55000", "203.0.113.45")
	req := httptest.NewRequest(http.MethodGet, "/admin/api/nodes", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("X-Forwarded-For", "203.0.113.45")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted proxy session status = %d", rec.Code)
	}

	for i := 0; i < 3; i++ {
		req = loginForm("admin", "wrong-password")
		req.RemoteAddr = "127.0.0.1:55000"
		req.Header.Set("X-Forwarded-For", "203.0.113.46")
		rec = httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("failure status = %d", rec.Code)
		}
	}
	masked, display := latestAdminBlockDisplay(t, server)
	if masked != "203.0.113.*" {
		t.Fatalf("masked ip = %s", masked)
	}
	if display != "203.0.113.46" {
		t.Fatalf("display ip = %s", display)
	}
}

func TestUntrustedProxyHeaderDoesNotChangeLoginIP(t *testing.T) {
	server, _ := newProxyTestServer(t, nil, config.AdminWeb{})
	for i := 0; i < 3; i++ {
		req := loginForm("admin", "wrong-password")
		req.RemoteAddr = "127.0.0.1:55000"
		req.Header.Set("X-Forwarded-For", "203.0.113.45")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("failure status = %d", rec.Code)
		}
	}
	masked, display := latestAdminBlockDisplay(t, server)
	if masked != "127.0.0.*" {
		t.Fatalf("masked ip = %s", masked)
	}
	if display != "127.0.0.1" {
		t.Fatalf("display ip = %s", display)
	}
}

func latestAdminBlockDisplay(t *testing.T, server *Server) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/security/blocks", nil)
	items, total, err := server.listBlocks(req, pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 || len(items) == 0 {
		t.Fatalf("expected active admin block, total=%d items=%#v", total, items)
	}
	masked, _ := items[0]["masked_ip"].(string)
	display, _ := items[0]["display_ip"].(string)
	return masked, display
}

func TestHighRiskWebSessionAllowedForRemoteAdmin(t *testing.T) {
	server, _ := newProxyTestServer(t, nil, config.AdminWeb{})
	projectsPath := filepath.Join(t.TempDir(), "projects.yaml")
	initial := config.Projects{Projects: []config.Project{{
		ID: "demo", Name: "演示项目", Repository: "owner/demo",
		Enabled: true, RetainVersions: 1, DownloadMultiplier: 1,
	}}}
	if err := writeProjectsFile(projectsPath, initial); err != nil {
		t.Fatal(err)
	}
	server.projects = mirrorsync.NewProjectLoader(projectsPath, initial)
	cookie := loginCookie(t, server, "192.0.2.10:55000", "")

	body, _ := json.Marshal(initial)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/projects", strings.NewReader(string(body)))
	req.RemoteAddr = "192.0.2.10:55000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, server.csrfToken(cookie.Value))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/nodes", nil)
	req.RemoteAddr = "192.0.2.11:55000"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("cross source status = %d location=%s", rec.Code, rec.Header().Get("Location"))
	}
}

func newProxyTestServer(t *testing.T, trusted []string, web config.AdminWeb) (*Server, *sql.DB) {
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
	body := "users:\n  - username: admin\n    password_hash: \"" +
		testPasswordHash("correct-password") + "\"\n"
	if err := os.WriteFile(usersPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	web.UsersFile = usersPath
	web.SessionSecretFile = filepath.Join(dir, "session.key")
	web.SessionTTL = "12h"
	web.LoginFailureWindow = "24h"
	web.LoginFailureLimit = 3
	web.LoginBanDuration = "168h"
	server, err := New(config.Administration{Web: web},
		mastercontrol.Repository{DB: db}, mirrorsync.Store{DB: db},
		Options{TrustedCIDRs: trusted})
	if err != nil {
		t.Fatal(err)
	}
	return server, db
}

func loginCookie(t *testing.T, server *Server, remote, forwarded string) *http.Cookie {
	t.Helper()
	req := loginForm("admin", "correct-password")
	req.RemoteAddr = remote
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionFrom(rec.Result().Cookies())
	if cookie == nil {
		t.Fatal("login did not create session cookie")
	}
	return cookie
}

func loginForm(username, password string) *http.Request {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
