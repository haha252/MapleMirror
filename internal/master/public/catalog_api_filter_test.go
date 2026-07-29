package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestCatalogAPIReturnsFilterGroupsTagsAndSuggestions(t *testing.T) {
	srv, cache := filteredCatalogTestServer(t)
	defer cache.close()
	req := httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?filter=software_type:launcher&filter=supported_system:linux", nil)
	rec := httptest.NewRecorder()
	srv.catalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, max-age=60" ||
		rec.Header().Get("ETag") == "" {
		t.Fatalf("浏览器缓存响应头错误：%v", rec.Header())
	}
	var body catalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.FilterGroups) != 2 || len(body.Projects) != 1 ||
		body.Projects[0].ProjectID != "p1" ||
		len(body.SuggestedProjects) != 1 || body.SuggestedProjects[0].ProjectID != "p2" {
		t.Fatalf("目录筛选响应错误：%+v", body)
	}
	if body.Projects[0].Tags["keywords"][0] != "ffmpeg" {
		t.Fatalf("未声明标签未返回：%+v", body.Projects[0].Tags)
	}
}

func TestCatalogAPIRejectsUnknownFilter(t *testing.T) {
	srv, cache := filteredCatalogTestServer(t)
	defer cache.close()
	rec := httptest.NewRecorder()
	srv.catalog(rec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?filter=missing:value", nil))
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), `"INVALID_REQUEST"`) {
		t.Fatalf("未知筛选项 status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCatalogAPIWithoutQueryKeepsCompleteLegacyProjectList(t *testing.T) {
	srv, cache := filteredCatalogTestServer(t)
	defer cache.close()
	rec := httptest.NewRecorder()
	srv.catalog(rec, httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog", nil))
	var body catalogResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil ||
		len(body.Projects) != 2 || len(body.SuggestedProjects) != 0 {
		t.Fatalf("无参数目录不兼容：status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCatalogServerCacheHitStillUsesPublicResourceLimit(t *testing.T) {
	srv, cache := filteredCatalogTestServer(t)
	defer cache.close()
	srv.ResourceLimiter = newPublicResourceLimiter(testResourceQuota(1, 100))
	for index := 0; index < 2; index++ {
		req := httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog?q=p1", nil)
		req.RemoteAddr = "198.51.100.9:1234"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if index == 0 && rec.Code != http.StatusOK {
			t.Fatalf("首次目录请求 status=%d body=%s", rec.Code, rec.Body.String())
		}
		if index == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("服务端缓存不应绕过限流：status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestCatalogETagWorksForFilteredPayload(t *testing.T) {
	srv, cache := filteredCatalogTestServer(t)
	defer cache.close()
	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog?q=ffmpeg", nil)
	srv.catalog(first, request)
	etag := first.Header().Get("ETag")
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/public/v1/catalog?q=ffmpeg", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	srv.catalog(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("筛选目录 ETag 未命中：status=%d body=%s", second.Code, second.Body.String())
	}
}

func filteredCatalogTestServer(t *testing.T) (Server, *catalogResultCache) {
	t.Helper()
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p2', '项目二', 'owner/two', 1, 1, 0, 1, 'hash2', 'now')`)
	filters := testCatalogFilters()
	projects := config.Projects{Projects: []config.Project{
		{ID: "p1", Name: "项目一", Enabled: true, Tags: map[string][]string{
			"software_type": {"launcher"}, "supported_system": {"linux"},
			"keywords": {"ffmpeg"},
		}},
		{ID: "p2", Name: "项目二", Enabled: true, Tags: map[string][]string{
			"software_type": {"launcher"}, "supported_system": {"windows"},
		}},
	}}
	cache := newCatalogResultCache(8 * 1024 * 1024)
	index := &catalogIndex{snapshot: newCatalogSnapshot(projects, filters, 1), cache: cache}
	return Server{Store: Store{DB: db}, CatalogIndex: index, CatalogCache: cache}, cache
}
