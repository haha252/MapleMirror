package public

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProjectStatsWindowsTimezoneAndIsolation(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "p1", "项目一", true)
	seedRankProject(t, db, "p2", "项目二", true)
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 10, 3, 16, 30, 0, 0, time.UTC)
	store := Store{DB: db, Location: loc}
	seedProjectStatDay(t, db, "p1", "2026-10-04", 10, 6, 4, 1024)
	seedProjectStatDay(t, db, "p1", "2026-09-05", 20, 12, 8, 2048)
	seedProjectStatDay(t, db, "p1", "2026-09-04", 30, 18, 12, 4096)
	seedProjectStatDay(t, db, "p1", "2026-08-06", 40, 24, 16, 8192)
	seedProjectStatDay(t, db, "p1", "2026-08-05", 50, 30, 20, 16384)
	seedProjectStatDay(t, db, "p1", "2026-10-05", 999, 999, 0, 999)
	seedProjectStatDay(t, db, "p2", "2026-10-04", 777, 700, 77, 999999)
	mustExec(t, db, `INSERT INTO project_stat_totals
		(project_id, authorization_count, web_authorization_count, api_authorization_count,
		transfer_started_count, sent_bytes, updated_at) VALUES ('p1', 150, 90, 60, 10, 31744, 'now')`)
	got, err := store.projectStatsAt(context.Background(), "p1", now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]projectMetric{
		"downloads": {150, 30, 70}, "web_downloads": {90, 18, 42},
		"api_downloads": {60, 12, 28}, "traffic": {31744, 3072, 12288},
	}
	for key, metric := range want {
		if got.Metrics[key] != metric {
			t.Fatalf("%s=%+v want %+v", key, got.Metrics[key], metric)
		}
	}
	if got.ProjectID != "p1" || got.Today != "2026-10-04" || got.Timezone != "Asia/Shanghai" || !got.HasData {
		t.Fatalf("unexpected project metadata: %+v", got)
	}
	if len(got.Trend) != 30 || got.Trend[0].Day != "2026-09-05" || got.Trend[29].Day != "2026-10-04" ||
		got.Trend[0].Downloads != 20 || got.Trend[29].Downloads != 10 || got.Trend[1].Downloads != 0 {
		t.Fatalf("unexpected continuous project trend: %+v", got.Trend)
	}
	for i := 1; i < len(got.Trend); i++ {
		if got.Trend[i].Day != dateOffset(got.Trend[i-1].Day, 1) {
			t.Fatal("trend dates are not consecutive")
		}
	}
}

func TestProjectStatsZeroAndInvisibleProjects(t *testing.T) {
	db := openMaster(t)
	seedRankProject(t, db, "empty", "空项目", true)
	seedRankProject(t, db, "disabled", "停用项目", false)
	store := Store{DB: db}
	got, err := store.ProjectStats(context.Background(), "empty")
	if err != nil || got.HasData || len(got.Trend) != 30 {
		t.Fatalf("empty stats: %+v err=%v", got, err)
	}
	for _, metric := range got.Metrics {
		if metric != (projectMetric{}) {
			t.Fatalf("empty project metric=%+v", metric)
		}
	}
	for _, id := range []string{"disabled", "missing"} {
		if _, err := store.ProjectStats(context.Background(), id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("invisible project %s err=%v", id, err)
		}
	}
}

func TestProjectStatsQueryUsesDateIndex(t *testing.T) {
	db := openMaster(t)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT stat_day, authorization_count
		FROM daily_project_stats WHERE project_id = ? AND stat_day BETWEEN ? AND ?
		ORDER BY stat_day`, "p1", "2026-08-06", "2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plans, " "), "SEARCH daily_project_stats USING INDEX") {
		t.Fatalf("expected bounded date index lookup, got %v", plans)
	}
}

func seedProjectStatDay(t *testing.T, db *sql.DB, id, day string, downloads, web, api, bytes int64) {
	t.Helper()
	mustExec(t, db, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes)
		VALUES (?, ?, ?, ?, ?, 0, ?)`, day, id, downloads, web, api, bytes)
}
