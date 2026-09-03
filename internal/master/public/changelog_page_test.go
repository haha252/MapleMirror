package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChangelogPageIncludesTimelineControlsAndNavigation(t *testing.T) {
	db := openMaster(t)
	rec := httptest.NewRecorder()
	Server{Store: Store{DB: db}}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/changelog", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("更新日志页面失败：status=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{
		`<title>枫源镜像更新日志 - 版本发布、功能改进与服务维护</title>`,
		`class="page-changelog"`,
		`class="site-scroll-region"`,
		`id="changelog-search-desktop"`,
		`id="changelog-search-mobile"`,
		`id="changelog-filters"`,
		`name="changelog-minimum-level"`,
		`data-i18n="changelog.all">展示全部</span>`,
		`data-i18n="changelog.notice">公告及以上</span>`,
		`data-i18n="changelog.warn">警告及以上</span>`,
		`data-i18n="changelog.critical">事故</span>`,
		`id="changelog-timeline"`,
		`class="download-layout"`,
		`class="catalog-left-rail"`,
		`class="catalog-mobile-toolbar"`,
		`/static/public/download-filters.css?v=`,
		`/static/public/download-filters-mobile.css?v=`,
		`/static/public/changelog.css?v=`,
		`/static/public/changelog.js?v=`,
		`icons.svg?v=`,
		`#changelog`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("更新日志页面缺少 %q：%s", want, body)
		}
	}
	timeline := strings.Index(body, `id="changelog-timeline"`)
	status := strings.Index(body, `id="changelog-status"`)
	if timeline < 0 || status < timeline {
		t.Fatalf("加载状态应位于时间线下方：%s", body)
	}
	api := strings.Index(body, `href="/api-docs"`)
	changelog := strings.Index(body, `href="/changelog"`)
	about := strings.Index(body, `href="/about"`)
	if api < 0 || changelog < api || about < changelog {
		t.Fatalf("顶栏顺序应为 API 文档、更新日志、关于：%s", body)
	}
}

func TestChangelogStaticAssetsSupportLazyLoadingAndAccessibleLevels(t *testing.T) {
	script := publicStaticBody(t, "changelog.js")
	for _, want := range []string{
		`window.setTimeout(loadInitial, 300)`,
		`window.MirrorCompat.createAbortController()`,
		`window.MirrorCompat.onMediaChange`,
		`new IntersectionObserver`,
		`document.querySelector(".changelog-content") : null`,
		`root: scrollRoot`,
		`rootMargin: "300px 0px"`,
		`error.code === "CHANGELOG_CHANGED"`,
		`article.setAttribute("aria-label", safeLevel`,
		`document.body.classList.toggle("catalog-filter-open", open)`,
		`timeZone: "Asia/Shanghai"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("更新日志脚本缺少 %q：%s", want, script)
		}
	}
	styles := publicStaticBody(t, "changelog.css")
	for _, want := range []string{
		`.changelog-entry--notice`,
		`.changelog-entry--warn`,
		`.changelog-entry--critical`,
		`--timeline-rail-width: 20px`,
		`--timeline-rail-width: 16px`,
		`left: calc(var(--timeline-rail-width) / 2)`,
		`grid-template-columns: var(--timeline-rail-width) minmax(0, 1fr)`,
		`justify-self: center`,
		`width: 8px`,
		`width: var(--timeline-node-size)`,
		`grid-template-columns: minmax(0, 1fr) auto`,
		`color: var(--text)`,
		`text-decoration: underline`,
		`@media (max-width: 680px)`,
	} {
		if !strings.Contains(styles, want) {
			t.Fatalf("更新日志样式缺少 %q：%s", want, styles)
		}
	}
	if !strings.Contains(script, `changelog-entry__card panel-card`) {
		t.Fatal("更新卡片应与时间轴标记使用独立网格列")
	}
	if strings.Contains(script, `changelog-entry__level`) ||
		strings.Contains(styles, `.changelog-entry__level`) {
		t.Fatal("更新卡片内部不应再渲染等级标签")
	}
	if strings.Contains(styles, `box-shadow: 0 0 0 2px var(--level-color)`) {
		t.Fatal("时间线节点不应再绘制外圈")
	}
	icons := publicStaticBody(t, "icons.svg")
	if !strings.Contains(icons, `symbol id="changelog"`) {
		t.Fatalf("更新日志图标缺失：%s", icons)
	}
}

func TestChangelogPageRejectsNonGETWithoutPageView(t *testing.T) {
	db := openMaster(t)
	rec := httptest.NewRecorder()
	Server{Store: Store{DB: db}}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/changelog", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("非 GET 更新日志页面应返回 405：status=%d body=%s", rec.Code, rec.Body.String())
	}
	var views int
	if err := db.QueryRow(`SELECT COUNT(*) FROM daily_site_stats`).Scan(&views); err != nil || views != 0 {
		t.Fatalf("非 GET 不应计入访问量：views=%d err=%v", views, err)
	}
}
