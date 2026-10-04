package public

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProjectStatsAPIVisibilityCacheAndAssetRoute(t *testing.T) {
	db := openMaster(t)
	seedStatsSnapshot(t, db)
	seedRankProject(t, db, "p2", "项目二", true)
	seedRankProject(t, db, "disabled", "停用项目", false)
	cache := &projectStatsCache{}
	srv := Server{Store: Store{DB: db}, ProjectStatsCache: cache}
	handler := srv.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	first := decodeProjectStats(t, get("/api/public/v1/projects/p1/stats"))
	if first.ProjectID != "p1" || first.Metrics["downloads"].Total != 3 || first.Metrics["traffic"].Total != 4096 {
		t.Fatalf("project must not use global/node traffic: %+v", first)
	}
	empty := decodeProjectStats(t, get("/api/public/v1/projects/p2/stats"))
	if empty.ProjectID != "p2" || empty.HasData || empty.Metrics["traffic"].Total != 0 {
		t.Fatalf("project cache leaked: %+v", empty)
	}
	mustExec(t, db, `UPDATE project_stat_totals SET authorization_count = 9 WHERE project_id = 'p1'`)
	if got := decodeProjectStats(t, get("/api/public/v1/projects/p1/stats")); got.Metrics["downloads"].Total != 3 {
		t.Fatalf("expected cached snapshot: %+v", got)
	}
	cache.mu.Lock()
	for _, item := range cache.entries {
		item.value.mu.Lock()
		item.value.expiresAt = time.Time{}
		item.value.mu.Unlock()
	}
	cache.mu.Unlock()
	if got := decodeProjectStats(t, get("/api/public/v1/projects/p1/stats")); got.Metrics["downloads"].Total != 9 {
		t.Fatalf("expected refreshed snapshot: %+v", got)
	}
	mustExec(t, db, `UPDATE projects SET enabled = 0 WHERE id = 'p1'`)
	for _, id := range []string{"p1", "disabled", "missing", "nested/project"} {
		if rec := get("/api/public/v1/projects/" + id + "/stats"); rec.Code != http.StatusNotFound {
			t.Fatalf("%s code=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}
	mustExec(t, db, `UPDATE projects SET enabled = 1 WHERE id = 'p1'`)
	if rec := get("/api/public/v1/projects/p1/assets"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "asset-1") {
		t.Fatalf("assets route regressed: %d %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/public/v1/projects/p1/stats", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported method code=%d", rec.Code)
	}
	if rec := get("/api/public/v1/projects/p1/stats/extra"); rec.Code != http.StatusNotFound {
		t.Fatalf("invalid project resource code=%d", rec.Code)
	}
}

func TestProjectStatsCacheBoundsAndPrunes(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "p1", "项目一", true)
	cache := &projectStatsCache{entries: map[string]projectStatsCacheEntry{}}
	now := timeNow()
	for i := 0; i < projectStatsCacheCapacity; i++ {
		key := time.Unix(int64(i), 0).String()
		cache.entries[key] = projectStatsCacheEntry{touched: now, value: &cachedStatsValue[projectStatsSnapshot]{}}
	}
	if _, err := cache.get(context.Background(), Store{DB: db}, "p1"); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != projectStatsCacheCapacity {
		t.Fatalf("cache size=%d", len(cache.entries))
	}
	for key, entry := range cache.entries {
		entry.touched = now.Add(-2 * time.Minute)
		cache.entries[key] = entry
	}
	if _, err := cache.get(context.Background(), Store{DB: db}, "p1"); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != 1 {
		t.Fatalf("expired project entries retained: %d", len(cache.entries))
	}
}

func TestProjectPageLoadsProjectStatisticsResources(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}}
	rec := httptest.NewRecorder()
	srv.downloadPage(rec, httptest.NewRequest(http.MethodGet, "/p1/", nil))
	for _, fragment := range []string{`class="project-columns"`, `class="project-content"`,
		`id="project-stats-metrics"`, `id="project-stats-chart"`, `data-project-id="p1"`,
		`/static/public/stats-metrics.js?v=`, `/static/public/stats-chart.js?v=`,
		`/static/public/project-stats.js?v=`, `/static/public/project-stats.css?v=`} {
		if !strings.Contains(rec.Body.String(), fragment) {
			t.Fatalf("project statistics missing %s", fragment)
		}
	}
	if strings.Contains(rec.Body.String(), `<script src="/static/public/stats.js?v=`) || strings.Contains(rec.Body.String(), `id="stats-nodes"`) {
		t.Fatal("project page must not load global data controller or node statistics")
	}
}

func decodeProjectStats(t *testing.T, rec *httptest.ResponseRecorder) projectStatsSnapshot {
	t.Helper()
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("project stats code=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	var envelope struct {
		Status string               `json:"status"`
		Data   projectStatsSnapshot `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Status != "success" {
		t.Fatalf("invalid project snapshot err=%v body=%s", err, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "page_views") || strings.Contains(rec.Body.String(), `"views"`) {
		t.Fatal("project snapshot must not contain global page views")
	}
	return envelope.Data
}
