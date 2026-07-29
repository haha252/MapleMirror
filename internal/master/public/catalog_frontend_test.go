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
		`class="site-header__right"`,
		`class="site-header__catalog"`,
		`id="catalog-search-desktop"`,
		`class="catalog-mobile-toolbar"`,
		`id="catalog-search-mobile"`,
		`id="catalog-filter-button"`,
		`<span>筛选器</span>`,
		`id="catalog-filters"`,
		`id="catalog-filter-clear"`,
		`#broom`,
		`<span>取消全部</span>`,
		`id="catalog-filter-backdrop"`,
		`id="catalog-suggestions"`,
		`id="catalog-suggestions-title"`,
		`没有严格匹配的项，但你可能在找：`,
		`id="suggested-project-cards"`,
		`class="project-tags"`,
		`/static/public/download-filters.css?v=`,
		`/static/public/download-filters-mobile.css?v=`,
		`/static/public/download-card.js?v=`,
		`/static/public/download-filters.js?v=`,
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
		`new AbortController()`,
		`requestController.abort()`,
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

func TestCatalogCardRendersOnlySelectedMatchingTags(t *testing.T) {
	body := publicStaticBody(t, "download-card.js")
	for _, want := range []string{
		`if (!options.selectedTags.has(key)) return`,
		`tag.className = "project-tag project-tag--active"`,
		`options.tagLabels.get(key) || value`,
		`tag.title = group + ": " + value`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("项目卡片缺少标签行为 %q：%s", want, body)
		}
	}
}

func TestCatalogStylesUseWideDesktopLayout(t *testing.T) {
	body := publicStaticBody(t, "download-filters.css")
	for _, want := range []string{
		`max-width: 1840px`,
		`grid-template-columns: 250px minmax(0, 1fr)`,
		`.catalog-left-rail`,
		`.catalog-page-notices`,
		`position: sticky`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("桌面搜索筛选样式缺少规则 %q：%s", want, body)
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
