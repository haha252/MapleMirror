package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/publiclocale"
)

func TestLocaleMessagesLoadForEveryRegisteredLocale(t *testing.T) {
	assets, err := loadDefaultWebAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range publiclocale.All() {
		messages := assets.localeMessages[locale.ID]
		if len(messages) < 300 {
			t.Fatalf("locale %q loaded only %d messages", locale.ID, len(messages))
		}
		for _, key := range []string{"brand.name", "nav.home", "catalog.filter", "project.back", "api.introTitle"} {
			if strings.TrimSpace(messages[key]) == "" {
				t.Fatalf("locale %q missing SSR message %q", locale.ID, key)
			}
		}
	}
}

func TestLocalizeRenderedHTMLTranslatesTextAttributesAndParams(t *testing.T) {
	input := []byte(`<!doctype html><html><body>
		<a data-i18n="greeting" data-i18n-params='{"name":"Maple"}'
			data-i18n-title="title" data-i18n-aria-label="label">默认文本</a>
		<input data-i18n-placeholder="placeholder" placeholder="默认占位">
	</body></html>`)
	messages := map[string]string{
		"greeting":    "Hello, {name}",
		"title":       "Open",
		"label":       "Open item",
		"placeholder": "Search",
	}
	got, err := localizeRenderedHTML(input, messages)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, want := range []string{
		`>Hello, Maple</a>`,
		`title="Open"`,
		`aria-label="Open item"`,
		`placeholder="Search"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("localized HTML missing %q: %s", want, body)
		}
	}
}

func TestEnglishIndexablePagesHaveEnglishSSRBody(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, PublicBaseURL: "https://fyhub.cn"}
	handler := srv.Handler()

	cases := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{
			path: "/en/",
			required: []string{
				`<html lang="en"`,
				`data-locale-prefix="en"`,
				`data-i18n="brand.primary">Maple</span>`,
				`data-i18n="brand.secondary">Mirror</span>`,
				`data-i18n="catalog.clear">Clear all</span>`,
				`data-i18n="catalog.filter">Filters</span>`,
				`Loading project list...`,
			},
			forbidden: []string{`>取消全部</span>`, `>筛选器</span>`, `正在加载项目列表...`},
		},
		{
			path: "/en/stats",
			required: []string{
				`data-i18n="stats.total">Totals</h2>`,
				`data-i18n="stats.popular">Popular resources</h3>`,
				`data-i18n="stats.nodes">Node information</h2>`,
			},
			forbidden: []string{`>总计信息</h2>`, `>热门资源排行</h3>`, `>节点信息</h2>`},
		},
		{
			path: "/en/api-docs",
			required: []string{
				`data-i18n="api.introTitle">Download integration</h2>`,
				`data-i18n="api.contractStableTitle">Stable developer endpoints</h3>`,
				`Method 1: redirect to the main-site verification page`,
			},
			forbidden: []string{`>下载接入方式</h2>`, `>稳定的开发者接口</h3>`, `方式一：跳转主站验证页下载`},
		},
		{
			path: "/en/changelog",
			required: []string{
				`data-i18n="changelog.filterLevel">Log level</span>`,
				`data-i18n="changelog.all">All entries</span>`,
				`data-i18n="changelog.loadMore"`,
				`>Load more</button>`,
			},
			forbidden: []string{`>日志等级</span>`, `>展示全部</span>`, `>加载更多</button>`},
		},
		{
			path: "/en/about",
			required: []string{
				`data-i18n="about.intro">About the project</h2>`,
				`data-i18n="about.repositoryPrefix">Open-source repository: </span>`,
				`data-i18n="about.sponsorSupport">Sponsorship</h2>`,
				`data-i18n="about.thanks">Acknowledgements</h2>`,
				`data-i18n="about.contribution">Contribute</h2>`,
			},
			forbidden: []string{`>项目简介</h2>`, `>赞助支持</h2>`, `>致谢</h2>`, `>贡献</h2>`},
		},
		{
			path: "/en/p1/",
			required: []string{
				`class="project-back"`,
				`href="/en/"`,
				`data-i18n="project.back">Back to Maple Mirror</span>`,
				`data-i18n="field.version">Version</span>`,
				`data-i18n="field.architecture">Architecture</span>`,
				`data-i18n="download.loading">Loading download component...</p>`,
				`data-i18n-alt="project.icon"`,
				`alt="项目一 icon"`,
			},
			forbidden: []string{`>返回枫源镜像</span>`, `>版本</span>`, `>架构</span>`, `正在加载下载组件...`, `alt="项目一 图标"`},
		},
	}

	for _, item := range cases {
		t.Run(item.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, item.path, nil)
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, body)
			}
			for _, want := range item.required {
				if !strings.Contains(body, want) {
					t.Fatalf("English SSR page %q missing %q: %s", item.path, want, body)
				}
			}
			for _, forbidden := range item.forbidden {
				if strings.Contains(body, forbidden) {
					t.Fatalf("English SSR page %q retained Chinese fallback %q: %s", item.path, forbidden, body)
				}
			}
		})
	}
}

func TestPublicFrontendLocaleRoutingHasNoEnglishSpecialCase(t *testing.T) {
	for _, name := range []string{"i18n-routing.js", "project.js", "download-card.js"} {
		body := publicStaticBody(t, name)
		for _, forbidden := range []string{
			`document.documentElement.lang === "en"`,
			`next === "en"`,
			`const localePrefix`,
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s still contains locale special case %q: %s", name, forbidden, body)
			}
		}
	}
	routing := publicStaticBody(t, "i18n-routing.js")
	for _, want := range []string{
		`getAttribute("data-locale-prefix")`,
		`localizePath: localizePath`,
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("registry-driven locale path support missing %q: %s", want, routing)
		}
	}
}
