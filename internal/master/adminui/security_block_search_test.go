package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlockSearchFindsContainingIPv4Segment(t *testing.T) {
	server, _ := newTestServer(t)
	create := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	if err := server.createBlock(create, "client", "192.0.2.0/24", "网段封禁", "168h"); err != nil {
		t.Fatal(err)
	}
	if err := server.createBlock(create, "client", "198.51.100.8", "单 IP", "168h"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet,
		"/admin/api/security/blocks?q=192.0.2.99", nil)
	items, total, err := server.listBlocks(req, pagination{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0]["display_ip"] != "192.0.2.0/24" {
		t.Fatalf("search result total=%d items=%#v", total, items)
	}
}

func TestBlockSearchKeepsFilteredPaginationTotal(t *testing.T) {
	server, _ := newTestServer(t)
	create := httptest.NewRequest(http.MethodPost, "/admin/api/security/blocks", nil)
	for _, ip := range []string{"192.0.2.1", "192.0.2.2", "198.51.100.1"} {
		if err := server.createBlock(create, "client", ip, "测试", "168h"); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet,
		"/admin/api/security/blocks?q=192.0.2&page=2&page_size=1", nil)
	items, total, err := server.listBlocks(req, pagination{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("filtered pagination total=%d len=%d items=%#v", total, len(items), items)
	}
}
