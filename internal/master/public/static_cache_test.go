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

func TestDownloadSelectorStaticSupportsHarmony(t *testing.T) {
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/download-selectors.js", nil))
	body := rec.Body.String()
	harmony := strings.Index(body, `return "harmony"`)
	linux := strings.Index(body, `return "linux"`)
	if rec.Code != http.StatusOK || harmony < 0 || linux < 0 || harmony >= linux ||
		!strings.Contains(body, `harmony: "鸿蒙"`) {
		t.Fatalf("鸿蒙识别应早于 Linux 且提供中文标签：status=%d body=%s", rec.Code, body)
	}
}

func TestDownloadSelectorStaticRemembersModePerProject(t *testing.T) {
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/download-selectors.js", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK ||
		!strings.Contains(body, `const selectionModeKeyPrefix = "mirror-selection-mode:v2:"`) ||
		!strings.Contains(body, `function selectionModeForProject(projectId, fallback)`) ||
		!strings.Contains(body, `function rememberSelectionMode(projectId, mode)`) ||
		!strings.Contains(body, `localStorage.getItem(selectionModeKeyPrefix + projectId)`) ||
		!strings.Contains(body, `localStorage.setItem(selectionModeKeyPrefix + projectId`) ||
		!strings.Contains(body, `return normalizeSelectionMode(fallback)`) {
		t.Fatalf("选择模式应按项目安全持久化并回退配置默认值：status=%d body=%s",
			rec.Code, body)
	}
}

func TestDownloadFileBrowserStaticUsesTwoLevels(t *testing.T) {
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/download-file-browser.js", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK ||
		!strings.Contains(body, `.file-browser__versions`) ||
		!strings.Contains(body, `.file-browser__files`) ||
		!strings.Contains(body, `function openVersion(version)`) ||
		!strings.Contains(body, `versionsView.hidden = true`) ||
		!strings.Contains(body, `filesView.hidden = false`) ||
		!strings.Contains(body, `function showDefault(asset, version)`) ||
		!strings.Contains(body, `versions.length === 1`) ||
		!strings.Contains(body, `backButton.hidden = true`) ||
		!strings.Contains(body, `showDefault: showDefault`) ||
		!strings.Contains(body, `showVersions: showVersions`) ||
		!strings.Contains(body, `file-browser__file--recommended`) ||
		!strings.Contains(body, `tag.textContent = "推荐下载"`) ||
		!strings.Contains(body, `button.disabled = !item.available`) ||
		!strings.Contains(body, `options.onDownload(item)`) ||
		!strings.Contains(body, `item.file_name`) {
		t.Fatalf("文件浏览器应逐级展示文件、标记推荐项并直接下载：status=%d body=%s",
			rec.Code, body)
	}
}

func TestDownloadPagesUseProjectDefaultModeAndSkipSingleVersionChoice(t *testing.T) {
	for _, name := range []string{"download.js", "project.js"} {
		rec := httptest.NewRecorder()
		Server{}.Handler().ServeHTTP(rec,
			httptest.NewRequest(http.MethodGet, "/static/public/"+name, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK ||
			!strings.Contains(body, `selectors.selectionModeForProject(`) ||
			!strings.Contains(body, `project.default_selection_mode`) ||
			!strings.Contains(body, `selectors.rememberSelectionMode(`) ||
			!strings.Contains(body, `item.dataset.selectionMode === mode`) ||
			!strings.Contains(body, `versionField.hidden = mode === "file" || versions.length <= 1`) ||
			!strings.Contains(body, `browser.showDefault(selectedAsset, versionSelect.value)`) {
			t.Fatalf("%s 应使用项目默认模式，并在单版本时跳过版本选择：status=%d body=%s",
				name, rec.Code, body)
		}
	}
}

func TestDownloadCardButtonRepresentsAvailability(t *testing.T) {
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/download.js", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK ||
		!strings.Contains(body, `button.disabled = !selected.available`) ||
		!strings.Contains(body, `actions.hidden = mode === "file"`) ||
		!strings.Contains(body, `selected.available ? "下载" : "暂不可下载"`) {
		t.Fatalf("下载按钮应同时表达资产可用状态：status=%d body=%s", rec.Code, body)
	}
}

func TestDownloadCardStaticKeepsLatestVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	Server{}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/public/download.js", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK ||
		!strings.Contains(body, `defaultVersion ? "最新版本：" + defaultVersion : "暂无版本"`) {
		t.Fatalf("卡片头部应固定展示项目最新版本：status=%d body=%s", rec.Code, body)
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
