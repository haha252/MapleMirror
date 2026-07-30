package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChangelogAPIFiltersSearchesAndPaginates(t *testing.T) {
	store := changelogTestStore(25)
	srv := Server{changelog: store}
	first := requestChangelog(t, srv, "/api/public/v1/changelog?minimum_level=notice&q=可见&limit=5")
	if len(first.Items) != 5 || first.NextCursor == "" {
		t.Fatalf("首批更新日志错误：%+v", first)
	}
	for _, item := range first.Items {
		if item.Level == "info" || !strings.Contains(item.Title, "可见") {
			t.Fatalf("等级或搜索筛选错误：%+v", item)
		}
	}
	if !strings.HasSuffix(first.Items[0].OccurredAt, "+08:00") {
		t.Fatalf("接口时间应带北京时间偏移：%+v", first.Items[0])
	}
	second := requestChangelog(t, srv, "/api/public/v1/changelog?minimum_level=notice&q=可见&limit=5&cursor="+
		url.QueryEscape(first.NextCursor))
	if len(second.Items) == 0 || second.Items[0].Title == first.Items[0].Title {
		t.Fatalf("游标续读重复或缺失：first=%+v second=%+v", first, second)
	}
}

func TestChangelogAPISearchesVisibleDescription(t *testing.T) {
	store := &changelogStore{snapshot: &changelogSnapshot{
		Generation: 1,
		Entries: []changelogEntry{{
			Level: "info", Title: "普通标题", SearchText: "普通标题 仅描述命中",
			OccurredAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.FixedZone("BJT", 8*3600)),
		}},
	}}
	response := requestChangelog(t, Server{changelog: store},
		"/api/public/v1/changelog?q=%E4%BB%85%E6%8F%8F%E8%BF%B0%E5%91%BD%E4%B8%AD")
	if len(response.Items) != 1 {
		t.Fatalf("搜索应覆盖可见描述文本：%+v", response)
	}
}

func TestChangelogAPIDefaultsToTwentyItems(t *testing.T) {
	response := requestChangelog(t, Server{changelog: changelogTestStore(25)},
		"/api/public/v1/changelog")
	if len(response.Items) != 20 || response.NextCursor == "" {
		t.Fatalf("默认批次应为 20：%+v", response)
	}
}

func TestChangelogAPIRejectsInvalidQueryAndChangedCursor(t *testing.T) {
	store := changelogTestStore(2)
	srv := Server{changelog: store}
	for _, path := range []string{
		"/api/public/v1/changelog?minimum_level=debug",
		"/api/public/v1/changelog?limit=51",
		"/api/public/v1/changelog?cursor=bad",
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应返回 400：status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	cursor := encodeChangelogCursor(1, "info", "", 1, 20)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/public/v1/changelog?cursor="+url.QueryEscape(cursor), nil))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "CHANGELOG_CHANGED") {
		t.Fatalf("旧快照游标应返回 409：status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChangelogAPIHandlesEmptyStore(t *testing.T) {
	response := requestChangelog(t, Server{}, "/api/public/v1/changelog")
	if response.Items == nil || len(response.Items) != 0 || response.NextCursor != "" {
		t.Fatalf("空目录响应应包含空数组：%+v", response)
	}
}

func changelogTestStore(count int) *changelogStore {
	entries := make([]changelogEntry, 0, count)
	levels := []string{"info", "notice", "warn", "critical"}
	for index := 0; index < count; index++ {
		entries = append(entries, changelogEntry{
			Level: levels[index%len(levels)], Title: "可见记录 " + strconv.Itoa(index),
			OccurredAt:      time.Date(2026, 7, 30, 12, index, 0, 0, time.FixedZone("BJT", 8*3600)),
			DescriptionHTML: "描述", SearchText: "可见记录 描述",
		})
	}
	return &changelogStore{snapshot: &changelogSnapshot{Generation: 2, Entries: entries}}
}

func requestChangelog(t *testing.T, srv Server, path string) changelogAPIResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("更新日志接口失败：status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data changelogAPIResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
