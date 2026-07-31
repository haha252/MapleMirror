package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicPagesLoadCompatibilityLayerFirst(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}
	for _, path := range []string{"/", "/stats", "/download/asset-1", "/changelog", "/about"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		compatAt := strings.Index(body, `/static/public/compat.js?v=`)
		themeAt := strings.Index(body, `/static/public/theme.js?v=`)
		if rec.Code != http.StatusOK || compatAt < 0 || themeAt < 0 || compatAt >= themeAt {
			t.Fatalf("%s 应在主题和页面脚本前加载兼容层：status=%d body=%s",
				path, rec.Code, body)
		}
	}
}

func TestCompatibilityLayerCoversLegacyWebViewAPIs(t *testing.T) {
	body := publicStaticBody(t, "compat.js")
	for _, want := range []string{
		`installReplaceChildren(window.Element`,
		`typeof window.AbortController === "function"`,
		`if (signal) options.signal = signal`,
		`typeof media.addEventListener === "function"`,
		`typeof media.addListener === "function"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("兼容层缺少 %q：%s", want, body)
		}
	}
}

func TestCatalogAndChangelogUseCompatibilityHelpers(t *testing.T) {
	for _, name := range []string{"download.js", "changelog.js"} {
		body := publicStaticBody(t, name)
		if !strings.Contains(body, `window.MirrorCompat.createAbortController()`) ||
			!strings.Contains(body, `window.MirrorCompat.withAbortSignal`) ||
			strings.Contains(body, `new AbortController()`) {
			t.Fatalf("%s 未完整降级请求取消：%s", name, body)
		}
	}
	for _, name := range []string{"download-filters.js", "changelog.js"} {
		body := publicStaticBody(t, name)
		if !strings.Contains(body, `window.MirrorCompat.onMediaChange`) {
			t.Fatalf("%s 未降级媒体查询变化监听：%s", name, body)
		}
	}
}

func TestDownloadPowLoadsMainThreadFallbackBeforeController(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	rec := httptest.NewRecorder()
	Server{Store: Store{DB: db}}.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/download/asset-1", nil))
	body := rec.Body.String()
	fallbackAt := strings.Index(body, `/static/public/vdf-fallback.js?v=`)
	controllerAt := strings.Index(body, `/static/public/download-pow.js?v=`)
	if rec.Code != http.StatusOK || fallbackAt < 0 || controllerAt < 0 || fallbackAt >= controllerAt {
		t.Fatalf("下载验证页应先加载主线程 VDF 降级：status=%d body=%s", rec.Code, body)
	}
}

func TestPunishmentPowKeepsWebCryptoFallback(t *testing.T) {
	body := publicStaticBody(t, "pow-loader.js")
	for _, want := range []string{
		`if (!window.Worker) return Promise.reject`,
		`return await solveWithWorkers(`,
		`falling back to single-threaded Web Crypto`,
		`return solveWithSubtle(challenge, difficulty, onAttempts, trackProgress)`,
		`crypto.subtle.digest("SHA-256", data)`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("惩罚 PoW 缺少 Worker/WASM 失败后的 Web Crypto 降级 %q：%s", want, body)
		}
	}
}
