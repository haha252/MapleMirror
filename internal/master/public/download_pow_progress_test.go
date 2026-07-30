package public

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadPowPageIncludesProgressLayers(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, body)
	}
	for _, want := range []string{
		`id="download-pow-status" class="status muted" role="status" aria-live="polite"`,
		`download-pow__status-copy--complete" aria-hidden="true"`,
		`class="download-pow__status-message"`,
		`class="download-pow__status-percent" hidden>0%</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected progress structure to include %q: %s", want, body)
		}
	}
	if count := strings.Count(body, `class="download-pow__status-copy`); count != 2 {
		t.Fatalf("expected two aligned status copies, got %d: %s", count, body)
	}
}

func TestDownloadPowProgressAssetsDeclarePlannedBehavior(t *testing.T) {
	assets, err := loadEmbeddedWebAssets()
	if err != nil {
		t.Fatal(err)
	}
	assertStaticContains(t, assets.staticFS, "pow-loader.js", []string{
		"const progressBatch = 8192;",
		"const progressInterval = 200;",
		"const subtleProgressBatch = 2048;",
		`self.postMessage({type: "progress", attempts: pending});`,
		"typeof options.onProgress",
		"onProgress(attempts);",
	})
	assertStaticContains(t, assets.staticFS, "download-pow.js", []string{
		"const chancePerAttempt = Math.pow(2, -bits);",
		"const target = Math.ceil(Math.log(0.2) / Math.log1p(-chancePerAttempt));",
		"if (count <= target) return 95 * count / target;",
		"return Math.min(99, 95 + 4 * (1 - Math.exp(-(count - target) / target)));",
		`setStatus("正在计算验证答案...", "muted", 0);`,
		`setStatus("验证计算完成，正在签发并同步下载令牌...", "muted", 100);`,
	})
	assertStaticContains(t, assets.staticFS, "download-verification.css", []string{
		"background: var(--accent-hover);",
		"color: #140f08;",
		"clip-path: inset(0 calc(100% - var(--download-pow-progress)) 0 0);",
		"@media (prefers-reduced-motion: reduce)",
	})
}

func assertStaticContains(t *testing.T, staticFS fs.FS, name string, wants []string) {
	t.Helper()
	data, err := fs.ReadFile(staticFS, name)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s should include %q", name, want)
		}
	}
}
