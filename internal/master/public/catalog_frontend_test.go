package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogPageIncludesDesktopAndMobileFilterSurfaces(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	rec := httptest.NewRecorder()
	Server{Store: Store{DB: db}}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`class="site-scroll-region"`,
		`</main>
</div>
<footer class="site-footer">`,
		`class="site-header__right"`,
		`class="site-header__catalog"`,
		`id="catalog-search-desktop"`,
		`class="catalog-mobile-toolbar"`,
		`id="catalog-search-mobile"`,
		`id="catalog-filter-button"`,
		`data-i18n="catalog.filter">筛选器</span>`,
		`id="catalog-filters"`,
		`id="catalog-filter-clear"`,
		`#broom`,
		`data-i18n="catalog.clear">取消全部</span>`,
		`id="catalog-filter-backdrop"`,
		`id="catalog-suggestions"`,
		`id="catalog-suggestions-title"`,
		`没有严格匹配的项，但你可能在找：`,
		`id="suggested-project-cards"`,
		`data-catalog-batch-rows="4"`,
		`data-catalog-prefetch-remaining-rows="1"`,
		`id="project-load-more"`,
		`id="suggested-project-load-more"`,
		`class="project-tags"`,
		`/static/public/download-filters.css?v=`,
		`/static/public/download-filters-mobile.css?v=`,
		`/static/public/download-card.js?v=`,
		`/static/public/download-filters.js?v=`,
		`/static/public/download-lazy.js?v=`,
	} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("首页缺少搜索筛选结构 %q：status=%d body=%s", want, rec.Code, body)
		}
	}
	cardAt := strings.Index(body, `/static/public/download-card.js?v=`)
	controllerAt := strings.Index(body, `/static/public/download.js?v=`)
	if cardAt < 0 || controllerAt < 0 || cardAt >= controllerAt {
		t.Fatalf("卡片模块必须在目录控制器前加载：%s", body)
	}
	searchAt := strings.Index(body, `class="site-header__catalog"`)
	themeAt := strings.Index(body, `class="theme-tools"`)
	if searchAt < 0 || themeAt < 0 || searchAt >= themeAt {
		t.Fatalf("桌面搜索框必须紧邻并位于主题工具左侧：%s", body)
	}
}

func TestCatalogControllerDebouncesCanonicalCachedRequests(t *testing.T) {
	body := publicStaticBody(t, "download.js")
	for _, want := range []string{
		`window.setTimeout(loadCatalog, 300)`,
		`new URLSearchParams()`,
		`params.set("q", search.toLowerCase())`,
		`filters.selectedFilters().forEach`,
		`cache: "default"`,
		`window.MirrorCompat.createAbortController()`,
		`window.MirrorCompat.withAbortSignal`,
		`initialController.abort()`,
		`error.status === 429`,
		`response.headers.get("Retry-After")`,
		`catalog.suggested_projects`,
		`suggestionsTitle.textContent = projects.length`,
		`"您可能还在找：" : "没有严格匹配的项，但你可能在找："`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("目录控制器缺少行为 %q：%s", want, body)
		}
	}
}

func TestCatalogControllerLoadsBothWaterfallsIncrementally(t *testing.T) {
	body := publicStaticBody(t, "download.js")
	for _, want := range []string{
		`params.set("page_size", String(pageSize))`,
		`params.set("cursor", cursor)`,
		`catalog.next_projects_cursor`,
		`catalog.next_suggested_projects_cursor`,
		`if (append) container.appendChild(fragment)`,
		`section.loading || !section.cursor`,
		`section.controller.abort()`,
		`error.code === "CATALOG_CHANGED"`,
		`section.button.hidden = lazyLoader.supported && !section.failed`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("目录控制器缺少懒加载行为 %q：%s", want, body)
		}
	}
	lazy := publicStaticBody(t, "download-lazy.js")
	for _, want := range []string{
		`new IntersectionObserver`,
		`window.matchMedia("(min-width: 1101px)").matches`,
		`document.querySelector(".catalog-results") : null`,
		`{root: scrollRoot}`,
		`columns(container) * batchRows`,
		`batchRows - remainingRows - 1`,
		`columns(section.container) * triggerRow`,
		`getBoundingClientRect().bottom <= 0`,
		`observer.observe(section.trigger)`,
		`window.addEventListener("resize"`,
	} {
		if !strings.Contains(lazy, want) {
			t.Fatalf("目录懒加载器缺少按列触发行为 %q：%s", want, lazy)
		}
	}
}

func TestCatalogFilterDrawerSupportsAccessibilityAndHistory(t *testing.T) {
	body := publicStaticBody(t, "download-filters.js")
	for _, want := range []string{
		`Array.from(selected).sort()`,
		`history.pushState({catalogFilters: true}`,
		`window.addEventListener("popstate"`,
		`event.key === "Escape"`,
		`event.key === "Tab"`,
		`document.body.classList.add("catalog-filter-open")`,
		`panel.setAttribute("aria-hidden", "true")`,
		`previousFocus.focus()`,
		`clearButton.disabled = selected.size === 0`,
		`if (clearSelections()) options.onChange()`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("移动筛选抽屉缺少行为 %q：%s", want, body)
		}
	}
}

func TestCatalogCardRendersSelectedAndSearchMatchingTags(t *testing.T) {
	body := publicStaticBody(t, "download-card.js")
	for _, want := range []string{
		`const search = String(options.search || "").trim().toLowerCase()`,
		`String(value).toLowerCase().includes(search)`,
		`String(label).toLowerCase() === search`,
		`if (!selected && !searchMatched) return`,
		`"project-tag--active" : "project-tag--search-match"`,
		`tag.title = group + ": " + value`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("项目卡片缺少标签行为 %q：%s", want, body)
		}
	}
	controller := publicStaticBody(t, "download.js")
	if !strings.Contains(controller, `search: filters.search()`) {
		t.Fatalf("目录控制器没有把当前搜索词传给卡片：%s", controller)
	}
}

func TestCatalogStylesUseWideDesktopLayout(t *testing.T) {
	body := publicStaticBody(t, "download-filters.css")
	for _, want := range []string{
		`max-width: 1840px`,
		`flex: 0 1 420px`,
		`width: min(420px, calc(100vw - 800px))`,
		`max-width: 420px`,
		`grid-template-columns: 250px minmax(0, 1fr)`,
		`.catalog-left-rail`,
		`.catalog-page-notices`,
		`position: sticky`,
		`.page-download .download-layout`,
		`.page-changelog .catalog-results`,
		`overflow-y: auto`,
		`position: static`,
		`@media (min-width: 1101px)`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("桌面搜索筛选样式缺少规则 %q：%s", want, body)
		}
	}
	header := publicStaticBody(t, "base-header.css")
	if !strings.Contains(header, `white-space: nowrap`) {
		t.Fatalf("桌面顶栏导航文字应禁止换行：%s", header)
	}
	for _, want := range []string{
		`.site-header__right {`,
		`min-width: 0;`,
		`.theme-tools {
  flex: 0 0 auto;`,
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("桌面顶栏缺少窄宽度布局规则 %q：%s", want, header)
		}
	}
	if !strings.Contains(header, `.site-header__catalog {
  min-width: 0;`) {
		t.Fatalf("桌面搜索框应允许在主题工具栏存在时收缩：%s", header)
	}
	shell := publicStaticBody(t, "base-shell.css")
	for _, want := range []string{
		`.page-download,`,
		`.page-changelog`,
		`height: 100dvh`,
		`overflow: hidden`,
		`.site-footer__filing`,
		`margin-bottom: 10px`,
		`font-size: 11px`,
	} {
		if !strings.Contains(shell, want) {
			t.Fatalf("页面独立滚动外壳缺少规则 %q：%s", want, shell)
		}
	}
}

func TestCatalogMobileStylesKeepDrawerScrollableAndHeaderSticky(t *testing.T) {
	body := publicStaticBody(t, "download-filters-mobile.css")
	for _, want := range []string{
		`@media (max-width: 1100px)`,
		`display: contents`,
		`position: fixed`,
		`overflow-y: auto`,
		`overscroll-behavior: contain`,
		`touch-action: pan-y`,
		`env(safe-area-inset-bottom)`,
		`position: sticky`,
		`top: -18px`,
		`body.catalog-filter-open`,
		`white-space: nowrap`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("搜索筛选样式缺少规则 %q：%s", want, body)
		}
	}
}

func publicStaticBody(t *testing.T, name string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/"+name, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
