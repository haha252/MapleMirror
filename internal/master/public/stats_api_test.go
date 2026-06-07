package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatsAPIIncludesDynamicSnapshot(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	day := statDay(timeNow(), time.Local)
	mustExec(t, db, `INSERT INTO daily_site_stats
		(stat_day, page_views, updated_at) VALUES ('`+day+`', 7, 'now')`)
	mustExec(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES ('`+day+`', 'p1', 3, 2, 4096)`)
	mustExec(t, db, `INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('`+day+`', 'asset-1', 3, 2, 4096, 'now')`)
	mustExec(t, db, `INSERT INTO daily_node_traffic_stats
		(stat_day, node_id, sent_bytes, updated_at) VALUES ('`+day+`', 'node-1', 8192, 'now')`)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/stats", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var body response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON response: %v body=%s", err, rec.Body.String())
	}
	data, ok := body.Data.(map[string]any)
	if rec.Code != http.StatusOK || !ok {
		t.Fatalf("expected successful stats snapshot, code=%d body=%s", rec.Code, rec.Body.String())
	}
	html := data["html"].(map[string]any)
	for _, want := range []string{"总访问量", "总下载量", "总流量"} {
		if !strings.Contains(html["metrics"].(string), want) {
			t.Fatalf("expected metrics html to include %q: %s", want, html["metrics"])
		}
	}
	if !strings.Contains(html["ranks"].(string), "项目一") ||
		!strings.Contains(html["nodes"].(string), "节点名称") {
		t.Fatalf("expected ranks and nodes html in snapshot: %+v", html)
	}
	if len(data["trend"].([]any)) != 30 {
		t.Fatalf("expected 30-day trend payload: %+v", data["trend"])
	}
}
