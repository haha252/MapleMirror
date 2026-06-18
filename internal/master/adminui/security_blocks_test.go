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
	var display string
	err := db.QueryRow(`SELECT masked_ip, display_ip FROM admin_ip_blocks WHERE ip_key = ?`,
		server.store.ipKey("192.0.2.10")).Scan(&masked, &display)
	if err != nil {
		t.Fatal(err)
	}
	if masked != "192.0.2.*" {
		t.Fatalf("masked ip = %s", masked)
	}
	if display != "192.0.2.10" {
		t.Fatalf("display ip = %s", display)
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

func TestManualClientBlockAcceptsIPv4Segment(t *testing.T) {
	server, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(req, "client", "192.0.2.9/24", "人工预封禁", "168h"); err != nil {
		t.Fatal(err)
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM client_blocks WHERE client_prefix_key = '192.0.2.0/24'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ipv4 segment client block count = %d, want 1", count)
	}
}

func TestManualClientBlockRejectsBroadCIDR(t *testing.T) {
	server, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	err := server.createBlock(req, "client", "192.0.0.0/16", "人工预封禁", "168h")
	if err == nil || !strings.Contains(err.Error(), "IPv4 /24") {
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

func TestListBlocksDisplaysFullSources(t *testing.T) {
	server, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(req, "admin", "192.0.2.10", "管理封禁", "168h"); err != nil {
		t.Fatal(err)
	}
	if err := server.createBlock(req, "client", "2001:db8::1", "下载封禁", "168h"); err != nil {
		t.Fatal(err)
	}

	items, total, err := server.listBlocks(req, pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("blocks total=%d len=%d", total, len(items))
	}

	want := map[string]bool{
		"192.0.2.10":      false,
		"2001:db8::1/128": false,
	}
	for _, item := range items {
		display, _ := item["display_ip"].(string)
		if _, ok := want[display]; ok {
			want[display] = true
		}
	}
	for display, ok := range want {
		if !ok {
			t.Fatalf("block source %s was not displayed fully: %#v", display, items)
		}
	}
}

func TestListBlocksDisplaysAutoAdminBlockIP(t *testing.T) {
	server, _ := newTestServer(t)
	for i := 0; i < 3; i++ {
		req := loginRequest("admin", "wrong-password")
		req.RemoteAddr = "198.51.100.23:55000"
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("failure status = %d", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/security/blocks", nil)
	items, total, err := server.listBlocks(req, pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("blocks total=%d len=%d", total, len(items))
	}
	if got := items[0]["display_ip"]; got != "198.51.100.23" {
		t.Fatalf("auto admin block display_ip = %v, item=%#v", got, items[0])
	}
	if got := items[0]["key"]; got == "198.51.100.23" {
		t.Fatalf("admin block key should stay hashed, item=%#v", items[0])
	}
}
