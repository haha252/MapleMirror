package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestDownloadPowPageIncludesAssetPayload(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, Notices: []config.PublicNotice{
		{Level: "critical", Message: "下载验证页公告"},
	}}

	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`<title>下载验证 - 枫源镜像</title>`,
		`"asset_id":"asset-1"`,
		`"project_name":"项目一"`,
		`"version":"v1"`,
		`"architecture":"amd64"`,
		`"size_bytes":12`,
		`/static/public/pow-loader.js`,
		`/static/public/download-pow.js`,
		`/static/public/wechat.png`,
		`/static/public/alipay.png`,
		`返回枫源镜像`,
		`下载站费用高昂，如有能力，欢迎捐赠！`,
		`page-notices page-notices--after`,
		`page-notice page-notice--critical`,
		`下载验证页公告`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected download verification page to include %q: %s", want, body)
		}
	}
	card := strings.Index(body, `download-pow panel-card`)
	notice := strings.Index(body, `下载验证页公告`)
	footer := strings.Index(body, `<footer class="site-footer">`)
	if card < 0 || notice < 0 || footer < 0 || !(card < notice && notice < footer) {
		t.Fatalf("download verification notice should be between content card and footer: %s", body)
	}
	for _, unwanted := range []string{
		`<header class="site-header">`,
		`完成浏览器验证后将自动开始下载`,
		`正在准备安全验证`,
		`<p class="muted">下载验证</p>`,
		`a.zip`,
		`返回首页`,
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("download verification page should not include %q: %s", unwanted, body)
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
		`<title>下载验证 - 枫源镜像</title>`,
		`"asset_id":"asset-1"`,
		`"project_name":"项目一"`,
		`"version":"v1"`,
		`"architecture":"amd64"`,
		`/static/public/download-pow.js`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected readable download page to include %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{
		`<header class="site-header">`,
		`完成浏览器验证后将自动开始下载`,
		`正在准备安全验证`,
		`<p class="muted">下载验证</p>`,
		`a.zip`,
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("readable download verification page should not include %q: %s", unwanted, body)
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
