package public

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadPageReloadsNoticesFromConfig(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("notices:\n  - level: notice\n    message: 旧公告\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := Server{Store: Store{DB: db}, NoticesPath: configPath, NoticeStore: newNoticeStore(nil)}

	first := httptest.NewRecorder()
	srv.downloadPage(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(first.Body.String(), `旧公告`) ||
		!strings.Contains(first.Body.String(), `page-notice--notice`) {
		t.Fatalf("expected initial notice from config: %s", first.Body.String())
	}

	if err := os.WriteFile(configPath, []byte("notices:\n  - level: critical\n    message: 新公告\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := httptest.NewRecorder()
	srv.downloadPage(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(second.Body.String(), `旧公告`) ||
		!strings.Contains(second.Body.String(), `新公告`) ||
		!strings.Contains(second.Body.String(), `page-notice--critical`) {
		t.Fatalf("expected reloaded notice from config: %s", second.Body.String())
	}
}
