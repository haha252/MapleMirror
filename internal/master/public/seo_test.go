package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPublicPagesUseFyhubApexSEOMetadata(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()
	cases := []struct {
		path, title, description, canonical, pageTitle, subtitle string
		staticPage                                               bool
	}{
		{"/", "枫源镜像 - GitHub Release 软件版本与文件下载服务", "枫源镜像是面向 GitHub Release 的公益镜像下载服务，提供免费、稳定、快速的软件版本与文件下载，支持项目搜索、版本筛选、镜像节点状态查看、网页验证下载和公共 API 接入，适用于网页用户、脚本工具与自动更新器。", "https://fyhub.cn/", "枫源镜像", "面向 GitHub Release 的公益镜像服务，提供稳定、快速的软件版本与文件下载。", true},
		{"/stats", "枫源镜像节点状态与下载数据统计 - 访问、流量与 SLA", "查看枫源镜像的访问量、下载量、传输流量、镜像节点在线状态与服务 SLA，了解最近 30 天的访问趋势、下载表现、节点健康状况和公共镜像服务运行情况，并为节点稳定性和下载服务可用性提供公开参考，便于用户了解服务质量。", "https://fyhub.cn/stats", "数据统计", "查看节点状态、访问量、下载量、流量与近 30 日趋势。", true},
		{"/api-docs", "枫源镜像公共下载 API 文档 - 项目、文件与自动下载接口", "枫源镜像公共下载 API 文档，介绍项目与文件查询、网页下载、程序下载、PoW 验证、授权令牌和自动更新器接入方式，帮助脚本、客户端、CI 和后端服务稳定获取 GitHub Release 文件，并支持集成方设计稳定的下载流程。", "https://fyhub.cn/api-docs", "API 文档", "面向网页、脚本、客户端与自动更新器的公开下载接口说明。", true},
		{"/changelog", "枫源镜像更新日志 - 版本发布、功能改进与服务维护", "查看枫源镜像的版本发布、功能更新、维护记录、服务调整与重要变更，了解镜像下载、公共 API、节点管理、安全策略和站点体验的最新改进，并按时间跟踪服务的持续变化与近期维护重点，帮助用户掌握服务演进方向。", "https://fyhub.cn/changelog", "更新日志", "按时间查看版本发布、功能更新、服务维护与重要变更。", true},
		{"/about", "关于枫源镜像 - 公益镜像服务、开源代码与赞助支持", "了解枫源镜像的公益目标、服务范围、开源代码、维护方式、赞助支持和问题反馈渠道，查看项目如何提供稳定、透明、可靠的 GitHub Release 下载服务，并参与共同建设，也欢迎用户参与节点贡献与社区支持。", "https://fyhub.cn/about", "关于本项目", "了解枫源镜像的公益目标、开源项目、维护方式与支持方式。", true},
		{"/p1/", "项目一 版本与文件下载 - 枫源镜像", "", "https://fyhub.cn/p1/", "项目一", "", false},
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
				!strings.Contains(body, `<h1 data-i18n-page-title="true">`+item.pageTitle+`</h1>`) ||
				!strings.Contains(body, `<meta name="description" content="`) ||
				!strings.Contains(body, `<link rel="canonical" href="`+item.canonical+`">`) {
				t.Fatalf("missing SEO metadata: %s", body)
			}
			if item.description != "" && !strings.Contains(body, `<meta name="description" content="`+item.description+`">`) {
				t.Fatalf("unexpected description metadata: %s", body)
			}
			if item.staticPage {
				if got := utf8.RuneCountInString(item.title); got < 25 || got > 80 {
					t.Fatalf("title rune length=%d, want 25..80: %q", got, item.title)
				}
				if got := utf8.RuneCountInString(item.description); got < 100 || got > 160 {
					t.Fatalf("description rune length=%d, want 100..160: %q", got, item.description)
				}
				if !strings.Contains(body, `<p class="page-subtitle" data-i18n-page-subtitle="true">`+item.subtitle+`</p>`) {
					t.Fatalf("missing SSR page subtitle: %s", body)
				}
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

func TestBrowserLocaleCannotRewriteIndexablePageSEO(t *testing.T) {
	i18n := publicStaticBody(t, "i18n.js")
	for _, want := range []string{
		`locale = canonical(document.documentElement.lang) || defaultLocale;`,
		`localStorage.setItem(storageKey, mode === "auto" ? "auto" : next);`,
		`if (syncSavedLocale()) return;`,
	} {
		if !strings.Contains(i18n, want) {
			t.Fatalf("stable locale handling missing %q: %s", want, i18n)
		}
	}
	routing := publicStaticBody(t, "i18n-routing.js")
	for _, want := range []string{
		`window.location.assign(url);`,
		`window.location.replace(url);`,
		`saved === "auto" ? browserLocale() : canonicalize(saved)`,
		`getAttribute("data-locale-routes")`,
		`JSON.parse(raw)`,
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("locale URL routing missing %q: %s", want, routing)
		}
	}
	for _, forbidden := range []string{
		`data-locale-en-path`,
		`data-locale-zh-path`,
		`next === "en"`,
	} {
		if strings.Contains(routing, forbidden) {
			t.Fatalf("locale routing must be registry-driven, found %q: %s", forbidden, routing)
		}
	}
	for _, forbidden := range []string{
		`locale = canonical(savedMode()) || browserLocale();`,
		`document.title =`,
	} {
		if strings.Contains(i18n, forbidden) {
			t.Fatalf("browser locale must not rewrite indexable SEO metadata, found %q: %s", forbidden, i18n)
		}
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
