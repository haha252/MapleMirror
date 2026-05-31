package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadPageIncludesButtonForAvailableAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `class="download-btn"`) {
		t.Fatalf("expected download button in page: %s", body)
	}
	if !strings.Contains(body, `data-asset-id="asset-1"`) {
		t.Fatalf("expected asset id in page: %s", body)
	}
	if !strings.Contains(body, `>下载<`) {
		t.Fatalf("expected download label in page: %s", body)
	}
}
