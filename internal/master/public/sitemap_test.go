package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSitemapIncludesPublicPagesAndEnabledProjectPages(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p two', '项目二', 'owner/two', 1, 1, 0, 1, 'hash-two', 'now')`)
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('disabled', '禁用项目', 'owner/disabled', 0, 1, 0, 1, 'hash-disabled', 'now')`)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "http://mirror.example.test/sitemap.xml", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("sitemap status=%d body=%s", rec.Code, body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/xml") {
		t.Fatalf("expected XML content type, got %q", got)
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`,
		`<loc>https://mirror.example.test/</loc>`,
		`<loc>https://mirror.example.test/en/</loc>`,
		`<loc>https://mirror.example.test/stats</loc>`,
		`<loc>https://mirror.example.test/en/stats</loc>`,
		`<loc>https://mirror.example.test/api-docs</loc>`,
		`<loc>https://mirror.example.test/en/api-docs</loc>`,
		`<loc>https://mirror.example.test/changelog</loc>`,
		`<loc>https://mirror.example.test/en/changelog</loc>`,
		`<loc>https://mirror.example.test/about</loc>`,
		`<loc>https://mirror.example.test/en/about</loc>`,
		`<loc>https://mirror.example.test/p1/</loc>`,
		`<loc>https://mirror.example.test/en/p1/</loc>`,
		`<loc>https://mirror.example.test/p%20two/</loc>`,
		`<loc>https://mirror.example.test/en/p%20two/</loc>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected sitemap to contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "disabled") {
		t.Fatalf("disabled project should not appear in sitemap: %s", body)
	}
}

func TestSitemapRejectsNonGETWithoutPageView(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodPost, "/sitemap.xml", nil)
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
		t.Fatalf("non-GET /sitemap.xml should not count page view: rows=%d err=%v", rows, err)
	}
}

func TestRobotsTXTIncludesSitemap(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodGet, "http://internal.example.test/robots.txt", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "mirror.example.test")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("robots status=%d body=%s", rec.Code, body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected text content type, got %q", got)
	}
	want := "User-agent: *\nAllow: /\nSitemap: https://mirror.example.test/sitemap.xml\n"
	if body != want {
		t.Fatalf("robots body=%q want %q", body, want)
	}
}

func TestRobotsTXTRejectsNonGETWithoutPageView(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	req := httptest.NewRequest(http.MethodPost, "/robots.txt", nil)
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
		t.Fatalf("non-GET /robots.txt should not count page view: rows=%d err=%v", rows, err)
	}
}
