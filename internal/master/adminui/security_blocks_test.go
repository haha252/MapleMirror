package adminui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManualAdminBlockUsesLoginIPKey(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(req, "admin", "192.0.2.10", "人工预封禁", "168h"); err != nil {
		t.Fatal(err)
	}

	var masked string
	err := db.QueryRow(`SELECT masked_ip FROM admin_ip_blocks WHERE ip_key = ?`,
		server.store.ipKey("192.0.2.10")).Scan(&masked)
	if err != nil {
		t.Fatal(err)
	}
	if masked != "192.0.2.*" {
		t.Fatalf("masked ip = %s", masked)
	}

	blockedLogin := loginRequest("admin", "correct-password")
	blockedLogin.RemoteAddr = "192.0.2.10:55000"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, blockedLogin)
	if sessionFrom(rec.Result().Cookies()) != nil {
		t.Fatal("manual admin block should stop matching login source")
	}

	allowedLogin := loginRequest("admin", "correct-password")
	allowedLogin.RemoteAddr = "192.0.2.11:55000"
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, allowedLogin)
	if rec.Code != http.StatusSeeOther || sessionFrom(rec.Result().Cookies()) == nil {
		t.Fatalf("unmatched source should still login, status=%d body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestManualAdminBlockRejectsCIDR(t *testing.T) {
	server, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	err := server.createBlock(req, "admin", "192.0.2.0/24", "人工预封禁", "168h")
	if err == nil || !strings.Contains(err.Error(), "只支持单个 IP") {
		t.Fatalf("expected cidr rejection, got %v", err)
	}
}

func TestManualClientBlockNormalizesSingleIP(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(req, "client", "192.0.2.10", "人工预封禁", "168h"); err != nil {
		t.Fatal(err)
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM client_blocks WHERE client_prefix_key = '192.0.2.10/32'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("normalized client block count = %d, want 1", count)
	}
}

func TestManualClientBlockRejectsBroadCIDR(t *testing.T) {
	server, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	err := server.createBlock(req, "client", "192.0.2.0/24", "人工预封禁", "168h")
	if err == nil || !strings.Contains(err.Error(), "只支持单 IP") {
		t.Fatalf("expected broad cidr rejection, got %v", err)
	}
}

func TestManualClientBlockAcceptsHostPrefix(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(req, "client", "2001:db8::1/128", "人工预封禁", "168h"); err != nil {
		t.Fatal(err)
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM client_blocks WHERE client_prefix_key = '2001:db8::1/128'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("host prefix client block count = %d, want 1", count)
	}
}
