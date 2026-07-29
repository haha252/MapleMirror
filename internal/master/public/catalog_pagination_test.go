package public

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestCatalogAPIPaginatesProjectsAndSuggestionsIndependently(t *testing.T) {
	srv := paginationTestServer(t)
	query := "filter=software_type:launcher&filter=supported_system:linux&page_size=2"
	first := catalogPageResponse(t, srv, query)
	if len(first.Projects) != 2 || len(first.SuggestedProjects) != 2 ||
		first.NextProjectsCursor == "" || first.NextSuggestedCursor == "" {
		t.Fatalf("首批目录分页错误：%+v", first)
	}

	nextProjects := catalogPageResponse(t, srv, query+"&cursor="+
		url.QueryEscape(first.NextProjectsCursor))
	if len(nextProjects.Projects) != 2 || len(nextProjects.SuggestedProjects) != 0 ||
		nextProjects.NextProjectsCursor != "" {
		t.Fatalf("主结果续页错误：%+v", nextProjects)
	}
	if first.Projects[0].ProjectID == nextProjects.Projects[0].ProjectID {
		t.Fatalf("主结果续页出现重复：first=%+v next=%+v", first, nextProjects)
	}
	resized := catalogPageResponse(t, srv,
		"filter=software_type:launcher&filter=supported_system:linux&page_size=1&cursor="+
			url.QueryEscape(first.NextProjectsCursor))
	if len(resized.Projects) != 1 || resized.NextProjectsCursor == "" {
		t.Fatalf("跨响应式断点后应允许调整续页数量：%+v", resized)
	}

	nextSuggestions := catalogPageResponse(t, srv, query+"&cursor="+
		url.QueryEscape(first.NextSuggestedCursor))
	if len(nextSuggestions.Projects) != 0 || len(nextSuggestions.SuggestedProjects) != 1 ||
		nextSuggestions.NextSuggestedCursor != "" {
		t.Fatalf("建议结果续页错误：%+v", nextSuggestions)
	}
}

func TestCatalogPaginationRejectsInvalidAndStaleCursors(t *testing.T) {
	srv := paginationTestServer(t)
	base := "filter=software_type:launcher&filter=supported_system:linux&page_size=2"
	first := catalogPageResponse(t, srv, base)
	cases := []string{
		base + "&cursor=invalid",
		"q=changed&page_size=2&cursor=" + url.QueryEscape(first.NextProjectsCursor),
		"page_size=7",
	}
	for _, query := range cases {
		rec := httptest.NewRecorder()
		srv.catalog(rec, httptest.NewRequest(http.MethodGet,
			"/api/public/v1/catalog?"+query, nil))
		if rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), `"INVALID_REQUEST"`) {
			t.Fatalf("非法分页参数未被拒绝：query=%s status=%d body=%s",
				query, rec.Code, rec.Body.String())
		}
	}

	srv.CatalogIndex.mu.Lock()
	srv.CatalogIndex.snapshot.Generation++
	srv.CatalogIndex.mu.Unlock()
	rec := httptest.NewRecorder()
	srv.catalog(rec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?"+base+"&cursor="+
			url.QueryEscape(first.NextProjectsCursor), nil))
	if rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), `"CATALOG_CHANGED"`) {
		t.Fatalf("过期游标状态错误：status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCatalogPaginationKeepsLegacyFullResponseAndDistinctETags(t *testing.T) {
	srv := paginationTestServer(t)
	fullRec := httptest.NewRecorder()
	srv.catalog(fullRec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog", nil))
	var full catalogResponse
	if fullRec.Code != http.StatusOK ||
		json.Unmarshal(fullRec.Body.Bytes(), &full) != nil || len(full.Projects) != 7 {
		t.Fatalf("无分页参数应保留完整目录：status=%d body=%s",
			fullRec.Code, fullRec.Body.String())
	}

	firstRec := httptest.NewRecorder()
	srv.catalog(firstRec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?page_size=2", nil))
	var first catalogResponse
	if json.Unmarshal(firstRec.Body.Bytes(), &first) != nil ||
		len(first.Projects) != 2 || first.NextProjectsCursor == "" {
		t.Fatalf("分页首批错误：status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	nextRec := httptest.NewRecorder()
	srv.catalog(nextRec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?page_size=2&cursor="+
			url.QueryEscape(first.NextProjectsCursor), nil))
	if firstRec.Header().Get("ETag") == "" ||
		firstRec.Header().Get("ETag") == nextRec.Header().Get("ETag") {
		t.Fatalf("分页响应应使用独立 ETag：first=%q next=%q",
			firstRec.Header().Get("ETag"), nextRec.Header().Get("ETag"))
	}
}

func catalogPageResponse(t *testing.T, srv Server, query string) catalogResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.catalog(rec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/catalog?"+query, nil))
	var response catalogResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil {
		t.Fatalf("目录分页请求失败：status=%d body=%s", rec.Code, rec.Body.String())
	}
	return response
}

func paginationTestServer(t *testing.T) Server {
	t.Helper()
	db := openMaster(t)
	filters := testCatalogFilters()
	var projects []config.Project
	for index := 0; index < 7; index++ {
		id := fmt.Sprintf("project-%d", index)
		if _, err := db.Exec(`INSERT INTO projects
			(id, name, repository, enabled, retain_versions, include_prerelease,
			download_multiplier, config_hash, updated_at)
			VALUES (?, ?, ?, 1, 1, 0, 1, 'hash', 'now')`,
			id, fmt.Sprintf("项目 %d", index), "owner/"+id); err != nil {
			t.Fatal(err)
		}
		tags := map[string][]string{"software_type": {"launcher"}}
		if index < 4 {
			tags["supported_system"] = []string{"linux"}
		} else {
			tags["supported_system"] = []string{"windows"}
		}
		projects = append(projects, config.Project{
			ID: id, Name: fmt.Sprintf("项目 %d", index), Enabled: true, Tags: tags,
		})
	}
	index := &catalogIndex{snapshot: newCatalogSnapshot(
		config.Projects{Projects: projects}, filters, 1)}
	cache := newCatalogResultCache(8 * 1024 * 1024)
	t.Cleanup(cache.close)
	return Server{
		Store: Store{DB: db}, CatalogIndex: index,
		CatalogCache: cache, CatalogBatchRows: 2,
	}
}
