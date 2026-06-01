package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageViewCookieCountsVisitorOncePerDay(t *testing.T) {
	db := openMaster(t)
	srv := Server{Store: Store{DB: db}, PageViews: newPageViewTracker()}
	first := httptest.NewRecorder()
	srv.aboutPage(first, httptest.NewRequest(http.MethodGet, "/about", nil))
	cookies := first.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != pageViewCookie {
		t.Fatalf("expected visitor cookie, got %+v", cookies)
	}

	aboutBody := first.Body.String()
	if !strings.Contains(aboutBody, `<title>关于本项目 - 枫源镜像</title>`) {
		t.Fatalf("expected about browser title: %s", aboutBody)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/stats", nil)
	secondReq.AddCookie(cookies[0])
	statsRec := httptest.NewRecorder()
	srv.statsPage(statsRec, secondReq)
	if !strings.Contains(statsRec.Body.String(), `<title>数据统计 - 枫源镜像</title>`) {
		t.Fatalf("expected stats browser title: %s", statsRec.Body.String())
	}

	var views int
	err := db.QueryRow(`SELECT page_views FROM daily_site_stats`).Scan(&views)
	if err != nil || views != 1 {
		t.Fatalf("同一访客同日只应计一次访问：views=%d err=%v", views, err)
	}
}

func TestPageViewTrackerResetsWhenDayChanges(t *testing.T) {
	tracker := newPageViewTracker()
	if !tracker.shouldCount("2026-05-31", "visitor-1") {
		t.Fatal("first visit should count")
	}
	if tracker.shouldCount("2026-05-31", "visitor-1") {
		t.Fatal("same visitor should not count twice in one day")
	}
	if !tracker.shouldCount("2026-06-01", "visitor-1") {
		t.Fatal("same visitor should count again on a new day")
	}
}
