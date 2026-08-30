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

func TestDownloadPowProgressAssetsDeclareVDFBehavior(t *testing.T) {
	assets, err := loadEmbeddedWebAssets()
	if err != nil {
		t.Fatal(err)
	}
	assertStaticContains(t, assets.staticFS, "vdf-worker.js", []string{
		"value = (value * value) % modulus;",
		`self.postMessage({type: "progress", completed: completed, iterations: iterations});`,
		"performance.now() - started",
		"const BYTE_LENGTH = 384;",
		"completed + 256",
		"now - lastReport >= 90",
	})
	workerData, err := fs.ReadFile(assets.staticFS, "vdf-worker.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workerData), "setTimeout(") {
		t.Fatal("vdf worker should not yield through timers while its isolated thread is solving")
	}
	assertStaticContains(t, assets.staticFS, "download-pow.js", []string{
		`typeof BigInt !== "function"`,
		`worker = new Worker(workerURL);`,
		`return await solveVDFInWorker(challenge);`,
		`return window.VDFFallback.solve(challenge`,
		"100 * message.completed / message.iterations",
		`postJSON("/api/public/v2/web/challenges"`,
		`postJSON("/api/public/v2/web/authorizations"`,
		`const successTemplate = document.getElementById("download-pow-success-template");`,
		`window.history.replaceState(null, "", successPath);`,
		`asset.success_path`,
		`stack.replaceChildren(successTemplate.content.cloneNode(true));`,
		`setStatus("正在计算验证答案...", "muted", 0);`,
		`setStatus("验证计算完成，正在签发并同步下载令牌...", "muted", 100);`,
	})
	assertStaticContains(t, assets.staticFS, "vdf-fallback.js", []string{
		"value = (value * value) % modulus;",
		"const BYTE_LENGTH = 384;",
		"window.setTimeout(resolve, 0)",
		"onProgress(completed, iterations)",
		"solve_elapsed_ms:",
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
