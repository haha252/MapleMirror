package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIDocsPageOnlyDocumentsPublicAPI(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodGet, "/api-docs", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	if !strings.Contains(body, `<title>API 文档 - 枫源镜像</title>`) {
		t.Fatalf("expected API docs browser title in page: %s", body)
	}
	for _, want := range []string{
		`<body class="page-api-docs">`,
		`方式一：跳转主站验证页下载`,
		`方式二：程序调用 API 下载`,
		`其他接口`,
		`href="#web-download-flow"`,
		`href="#api-download-flow"`,
		`href="#other-public-apis"`,
		`主站地址，不是下载节点地址`,
		`/api/public/v1/projects`,
		`/api/public/v1/projects/{project_id}/assets`,
		`/api/public/v2/api/challenges`,
		`/api/public/v2/api/authorizations`,
		`API V1 计算与验证方式弃用提醒`,
		`SHA-256 前导零 nonce 搜索`,
		`不能只替换接口路径`,
		`/api/public/v1/api/challenges`,
		`/api/public/v1/api/authorizations`,
		`class="api-danger-note" role="alert"`,
		`rsa-repeated-squaring-v1`,
		`展开了解 RSA repeated-squaring 的计算原理`,
		`id="api-vdf-principle"`,
		`复制原理Markdown`,
		`data-copy-markdown="api-vdf-principle-markdown"`,
		`repeat exactly iterations times:`,
		`/static/public/api-docs.js`,
		`/api/public/v1/blocklist.txt`,
		`/api/public/v1/blocklist.json`,
		`/api/public/v1/changelog`,
		`/{project_id}/{version}/{file_name}`,
		`download_url`,
		`Authorization: Bearer &lt;download_token&gt;`,
		`class="api-method method-get"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected API docs page to include %q: %s", want, body)
		}
	}
	if strings.Contains(body, "/api/admin/v1") {
		t.Fatalf("API docs page must not document admin API: %s", body)
	}
	home := strings.Index(body, `href="/"`)
	stats := strings.Index(body, `href="/stats"`)
	docs := strings.Index(body, `href="/api-docs"`)
	about := strings.Index(body, `href="/about"`)
	if home < 0 || stats < 0 || docs < 0 || about < 0 || !(home < stats && stats < docs && docs < about) {
		t.Fatalf("expected nav order home, stats, API docs, about: %s", body)
	}
}

func TestAPIDocsCopyAssetsDeclareMarkdownBehavior(t *testing.T) {
	assets, err := loadEmbeddedWebAssets()
	if err != nil {
		t.Fatal(err)
	}
	assertStaticContains(t, assets.staticFS, "api-docs.js", []string{
		`document.querySelectorAll("[data-copy-markdown]")`,
		`navigator.clipboard.writeText(text)`,
		`document.execCommand("copy")`,
		`source.textContent.trim()`,
	})
	assertStaticContains(t, assets.staticFS, "api-docs-copy.css", []string{
		".api-details-actions", ".api-copy-markdown", ".api-principle-points", ".api-danger-note",
	})
}

func TestAPIDocsPageRejectsNonGETWithoutPageView(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodPost, "/api-docs", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("expected JSON method error, body=%s", rec.Body.String())
	}
	var rows int
	err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&rows)
	if err != nil || rows != 0 {
		t.Fatalf("non-GET /api-docs should not count page view: rows=%d err=%v", rows, err)
	}
}
