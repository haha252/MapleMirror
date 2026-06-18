package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicPagesUseVersionedStaticURLs(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	for _, path := range []string{"/", "/stats", "/download/asset-1", "/about", "/api-docs"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, body)
		}
		if !strings.Contains(body, `/static/public/logo.webp?v=`) ||
			!strings.Contains(body, `/static/public/theme.js?v=`) {
			t.Fatalf("%s should use versioned public static URLs: %s", path, body)
		}
		if strings.Contains(body, `/static/public/icons.svg#`) {
			t.Fatalf("%s should put static version before SVG fragment: %s", path, body)
		}
		if strings.Contains(body, `<link rel="stylesheet" href="/static/public/base.css`) {
			t.Fatalf("%s should not request aggregate base.css: %s", path, body)
		}
	}
}

func TestPublicStaticUsesImmutableCache(t *testing.T) {
	srv := Server{}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/public/download.js?v=test", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("static status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func TestCatalogETag(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	first := httptest.NewRecorder()
	srv.catalog(first, httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog", nil))
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("catalog status=%d etag=%q body=%s", first.Code, etag, first.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	srv.catalog(second, req)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("expected 304 without body, status=%d body=%s", second.Code, second.Body.String())
	}
}
