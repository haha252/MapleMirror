package public

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatsAPIsSplitFastAndDetailsSnapshots(t *testing.T) {
	db := openMaster(t)
	seedStatsSnapshot(t, db)
	srv := Server{Store: Store{DB: db}}

	fastRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(fastRec, httptest.NewRequest(http.MethodGet, "/api/public/v1/stats", nil))
	var fast statsFastSnapshot
	decodeStatsResponse(t, fastRec, &fast)
	if fast.Metrics[0] != [3]int64{7, 7, 0} || fast.Metrics[1] != [3]int64{3, 3, 0} ||
		fast.Metrics[2] != [3]int64{4096, 4096, 0} || fast.Today[1].(float64) != 7 {
		t.Fatalf("unexpected fast stats snapshot: %+v", fast)
	}
	if strings.Contains(fastRec.Body.String(), `"r"`) || strings.Contains(fastRec.Body.String(), `"n"`) {
		t.Fatalf("fast stats snapshot should not include low-frequency data: %s", fastRec.Body.String())
	}

	detailsRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/stats/details", nil)
	srv.Handler().ServeHTTP(detailsRec, req)
	var details statsDetailsSnapshot
	decodeStatsResponse(t, detailsRec, &details)
	if len(details.Ranks) != 1 || details.Ranks[0][3].(float64) != 3 {
		t.Fatalf("expected compact rank rows: %+v", details.Ranks)
	}
	if len(details.Nodes) != 1 || details.Nodes[0][3].(float64) != 1 ||
		details.Nodes[0][7].(float64) != 8192 {
		t.Fatalf("expected compact node rows: %+v", details.Nodes)
	}
	if len(details.Trend.Views) != 30 || len(details.Trend.Downloads) != 30 ||
		len(details.Trend.Bytes) != 30 {
		t.Fatalf("expected 30-day compact trend payload: %+v", details.Trend)
	}
}

func TestStatsAPIGzipCompression(t *testing.T) {
	db := openMaster(t)
	seedStatsSnapshot(t, db)
	srv := Server{Store: Store{DB: db}}

	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/stats", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip stats response, headers=%v", rec.Header())
	}
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("expected gzip body: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	var snapshot statsFastSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatalf("expected compressed JSON snapshot: %v body=%s", err, string(body))
	}
}

func seedStatsSnapshot(t *testing.T, db *sql.DB) {
	t.Helper()
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
}

func decodeStatsResponse(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected successful stats snapshot, code=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<") || strings.Contains(rec.Body.String(), `"html"`) ||
		strings.Contains(rec.Body.String(), `"data"`) {
		t.Fatalf("stats snapshot should not include HTML or response envelope: %s", rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("expected compact JSON response: %v body=%s", err, rec.Body.String())
	}
}
