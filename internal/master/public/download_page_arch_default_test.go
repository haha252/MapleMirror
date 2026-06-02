package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadPageIncludesArchitectureDefaultFlag(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, ProjectAssets: map[string]projectAssetConfig{
		"p1": {ArchitectureDefaultEnabled: true},
	}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"architecture_default_enabled":true`) {
		t.Fatalf("expected architecture default flag in payload: %s", body)
	}
}
