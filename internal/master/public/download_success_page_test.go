package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadSuccessPageUsesReadablePathAndNoVerificationScripts(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/download/success/p1/v1/a.zip", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("success page must not be cached: %q", rec.Header().Get("Cache-Control"))
	}
	for _, want := range []string{
		`<title>下载已开始 - 枫源镜像</title>`,
		`class="page-download-success page-download-pow"`,
		`验证已通过，下载已开始。`,
		`下载已开始。关闭或重新打开浏览器不会再次自动下载。`,
		`href="/p1/v1/a.zip"`,
		`重新下载`,
		`返回枫源镜像`,
		`/static/public/donate-oc.webp`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("success page should include %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{
		`"asset_id"`,
		`/static/public/download-pow.js`,
		`/static/public/vdf-fallback.js`,
		`id="download-pow-asset"`,
		`download/success//`,
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("success page should not include %q: %s", unwanted, body)
		}
	}
}

func TestDownloadSuccessPagePreservesHomeRetryContext(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/download/success/p1/v1/a.zip?from=home", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(),
		`href="/p1/v1/a.zip?from=home"`) {
		t.Fatalf("success retry should preserve from=home: status=%d body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestDownloadSuccessPageRejectsInvalidPathsAndMethods(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}
	for _, path := range []string{
		"/download/success/p1/v1/missing.zip",
		"/download/success/p1/v1/a.zip/extra",
		"/download/success/p1/%2e%2e/a.zip",
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("invalid success path %q returned %d: %s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/download/success/p1/v1/a.zip", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST success path returned %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadSuccessPathUsesCanonicalReadablePath(t *testing.T) {
	got, err := downloadSuccessPath("/p1/v1/a.zip")
	if err != nil || got != "/download/success/p1/v1/a.zip" {
		t.Fatalf("success path = %q, err=%v", got, err)
	}
	if _, err := downloadSuccessPath("/p1/v1"); err == nil {
		t.Fatal("invalid readable path should be rejected")
	}
}
