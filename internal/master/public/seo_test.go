package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicPagesUseFyhubApexSEOMetadata(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()
	cases := []struct {
		path, title, canonical string
	}{
		{"/", "枫源镜像 - GitHub Release 镜像下载", "https://fyhub.cn/"},
		{"/stats", "镜像节点与下载数据统计 - 枫源镜像", "https://fyhub.cn/stats"},
		{"/api-docs", "枫源镜像公共下载 API 文档", "https://fyhub.cn/api-docs"},
		{"/changelog", "枫源镜像更新日志 - 功能与维护记录", "https://fyhub.cn/changelog"},
		{"/about", "关于枫源镜像 - 公益镜像服务与开源项目", "https://fyhub.cn/about"},
		{"/p1/", "项目一 版本与文件下载 - 枫源镜像", "https://fyhub.cn/p1/"},
	}
	for _, item := range cases {
		t.Run(item.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, item.path, nil))
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, body)
			}
			if !strings.Contains(body, "<title>"+item.title+"</title>") ||
				!strings.Contains(body, `<meta name="description" content="`) ||
				!strings.Contains(body, `<link rel="canonical" href="`+item.canonical+`">`) {
				t.Fatalf("missing SEO metadata: %s", body)
			}
			if strings.Contains(body, "www.fyhub.cn") {
				t.Fatalf("SEO output must not mention www.fyhub.cn: %s", body)
			}
		})
	}

	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(home.Body.String(), `data-i18n-alt="project.iconFallback" alt="项目图标"`) ||
		!strings.Contains(home.Body.String(), `alt="" aria-hidden="true"`) {
		t.Fatalf("content and decorative image ALT markup missing: %s", home.Body.String())
	}
}

func TestRestrictedAndVerificationPagesAreNoindex(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}}
	cases := []struct {
		name   string
		render func(*httptest.ResponseRecorder, *http.Request)
	}{
		{"download", func(w *httptest.ResponseRecorder, r *http.Request) {
			srv.renderDownloadPowPage(w, r, DownloadAssetSummary{AssetID: "asset-1", Available: true})
		}},
		{"blocked", func(w *httptest.ResponseRecorder, r *http.Request) {
			srv.renderBlockedPage(w, r, blockDecision{})
		}},
		{"punishment", func(w *httptest.ResponseRecorder, r *http.Request) {
			srv.renderPunishmentPage(w, r, blockDecision{})
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			record := httptest.NewRecorder()
			item.render(record, httptest.NewRequest(http.MethodGet, "/", nil))
			if !strings.Contains(record.Body.String(), `<meta name="robots" content="noindex,nofollow">`) {
				t.Fatalf("restricted page is indexable: %s", record.Body.String())
			}
		})
	}
}

func TestIndexNowKeyRouteReturnsPlainTextKey(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	srv := Server{IndexNowKey: key}
	request := httptest.NewRequest(http.MethodGet, "/"+key+".txt", nil)
	record := httptest.NewRecorder()
	srv.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK || record.Body.String() != key ||
		!strings.Contains(record.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("key route response code=%d type=%q body=%q", record.Code, record.Header().Get("Content-Type"), record.Body.String())
	}

	method := httptest.NewRecorder()
	srv.Handler().ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/"+key+".txt", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("key route POST status=%d body=%s", method.Code, method.Body.String())
	}
}

func TestConfiguredPublicOriginIsUsedByRobotsAndSitemap(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	record := httptest.NewRecorder()
	srv.Handler().ServeHTTP(record, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	want := "User-agent: *\nAllow: /\nSitemap: https://fyhub.cn/sitemap.xml\n"
	if record.Code != http.StatusOK || record.Body.String() != want {
		t.Fatalf("robots output=%q want %q", record.Body.String(), want)
	}
	sitemap := httptest.NewRecorder()
	srv.Handler().ServeHTTP(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if sitemap.Code != http.StatusOK || strings.Contains(sitemap.Body.String(), "www.fyhub.cn") ||
		!strings.Contains(sitemap.Body.String(), "https://fyhub.cn/api-docs") {
		t.Fatalf("sitemap output=%d %s", sitemap.Code, sitemap.Body.String())
	}
}
