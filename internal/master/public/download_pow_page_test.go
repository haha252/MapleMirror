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
		`/static/public/wechat.png`,
		`/static/public/alipay.png`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected download verification page to include %q: %s", want, body)
		}
	}
}

func TestReadableDownloadPowPageIncludesAssetPayload(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`<title>a.zip - 下载验证</title>`,
		`"asset_id":"asset-1"`,
		`"download_path":"/p1/v1/a.zip"`,
		`/static/public/download-pow.js`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected readable download page to include %q: %s", want, body)
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

func TestReadableDownloadPowPageRejectsMissingAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/p1/v1/missing.zip", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadPowPageCountsPageView(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PageViews: newPageViewTracker()}

	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != pageViewCookie {
		t.Fatalf("expected visitor cookie, got %+v", cookies)
	}
	var views int
	err := db.QueryRow(`SELECT page_views FROM daily_site_stats`).Scan(&views)
	if err != nil || views != 1 {
		t.Fatalf("下载验证页应计入访问量：views=%d err=%v", views, err)
	}
}

func TestDownloadPowPagesDeduplicateSameVisitor(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PageViews: newPageViewTracker()}

	first := httptest.NewRecorder()
	srv.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/download/asset-1", nil))
	cookies := first.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != pageViewCookie {
		t.Fatalf("expected visitor cookie, got %+v", cookies)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip", nil)
	secondReq.AddCookie(cookies[0])
	second := httptest.NewRecorder()
	srv.Handler().ServeHTTP(second, secondReq)
	if second.Code != http.StatusOK {
		t.Fatalf("expected readable download page 200, got %d: %s", second.Code, second.Body.String())
	}

	var views int
	err := db.QueryRow(`SELECT page_views FROM daily_site_stats`).Scan(&views)
	if err != nil || views != 1 {
		t.Fatalf("同一访客同日访问多个下载验证页只应计一次：views=%d err=%v", views, err)
	}
}
