package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadCatalogPreservesLatestPublishedTime(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	const publishedAt = "2026-06-26T08:17:42.123Z"
	mustExec(t, db, `UPDATE releases SET published_at = ? WHERE id = 'rel-1'`, publishedAt)
	srv := Server{Store: Store{DB: db}}

	catalog := string(catalogBody(t, srv))
	if !strings.Contains(catalog, `"latest_published_at":"`+publishedAt+`"`) {
		t.Fatalf("catalog should preserve the release time, body=%s", catalog)
	}

	rec := httptest.NewRecorder()
	srv.downloadPage(rec, httptest.NewRequest(http.MethodGet, "/p1/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(),
		`data-updated-at="`+publishedAt+`"`) {
		t.Fatalf("project page should preserve the release time, code=%d body=%s", rec.Code, rec.Body.String())
	}
}
