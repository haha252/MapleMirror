package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadPowPageIncludesAssetPayload(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`<title>a.zip - 下载验证</title>`,
		`"asset_id":"asset-1"`,
		`"file_name":"a.zip"`,
		`/static/public/pow-loader.js`,
		`/static/public/download-pow.js`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected download verification page to include %q: %s", want, body)
		}
	}
}

func TestDownloadPowPageRejectsMissingAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/download/missing", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadPowPageDoesNotCountHomePageView(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var rows int
	err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&rows)
	if err != nil || rows != 0 {
		t.Fatalf("download verification page should not count as home view: rows=%d err=%v", rows, err)
	}
}
