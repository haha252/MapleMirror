package public

import (
	"context"
	"database/sql"
	"time"
)

type projectMetric struct {
	Total    int64 `json:"total"`
	Recent   int64 `json:"recent"`
	Previous int64 `json:"previous"`
}

type projectStatsSnapshot struct {
	ProjectID   string                   `json:"project_id"`
	Timezone    string                   `json:"timezone"`
	GeneratedAt string                   `json:"generated_at"`
	Today       string                   `json:"today"`
	HasData     bool                     `json:"has_data"`
	Metrics     map[string]projectMetric `json:"metrics"`
	Trend       []projectDailyTrend      `json:"trend"`
}

type projectDailyTrend struct {
	Day          string `json:"day"`
	Downloads    int64  `json:"downloads"`
	WebDownloads int64  `json:"web_downloads"`
	APIDownloads int64  `json:"api_downloads"`
	SentBytes    int64  `json:"sent_bytes"`
}

func (s Store) ProjectStats(ctx context.Context, projectID string) (projectStatsSnapshot, error) {
	return s.projectStatsAt(ctx, projectID, timeNow())
}

func (s Store) projectStatsAt(ctx context.Context, projectID string, now time.Time) (projectStatsSnapshot, error) {
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	today := statDay(now, loc)
	start, previousStart := dateOffset(today, -29), dateOffset(today, -59)
	out := projectStatsSnapshot{ProjectID: projectID, Timezone: loc.String(),
		GeneratedAt: now.UTC().Format(time.RFC3339Nano), Today: today,
		Metrics: make(map[string]projectMetric)}
	readCtx, cancel := stableDatabaseReadContext(ctx)
	defer cancel()
	tx, err := s.DB.BeginTx(readCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var downloads, web, api, traffic projectMetric
	err = tx.QueryRowContext(readCtx, `SELECT COALESCE(t.authorization_count, 0),
		COALESCE(t.web_authorization_count, 0), COALESCE(t.api_authorization_count, 0),
		COALESCE(t.sent_bytes, 0) FROM projects p
		LEFT JOIN project_stat_totals t ON t.project_id = p.id
		WHERE p.id = ? AND p.enabled = 1`, projectID).
		Scan(&downloads.Total, &web.Total, &api.Total, &traffic.Total)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(readCtx, `SELECT stat_day, authorization_count,
		web_authorization_count, api_authorization_count, sent_bytes
		FROM daily_project_stats WHERE project_id = ? AND stat_day BETWEEN ? AND ?
		ORDER BY stat_day`, projectID, previousStart, today)
	if err != nil {
		return out, err
	}
	points := make(map[string]projectDailyTrend)
	for rows.Next() {
		var point projectDailyTrend
		if err := rows.Scan(&point.Day, &point.Downloads, &point.WebDownloads,
			&point.APIDownloads, &point.SentBytes); err != nil {
			rows.Close()
			return out, err
		}
		points[point.Day] = point
		values := []int64{point.Downloads, point.WebDownloads, point.APIDownloads, point.SentBytes}
		for i, metric := range []*projectMetric{&downloads, &web, &api, &traffic} {
			if point.Day >= start {
				metric.Recent += values[i]
			} else {
				metric.Previous += values[i]
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	for _, day := range daysBetween(start, today) {
		point := points[day]
		point.Day = day
		out.Trend = append(out.Trend, point)
	}
	out.Metrics["downloads"], out.Metrics["web_downloads"] = downloads, web
	out.Metrics["api_downloads"], out.Metrics["traffic"] = api, traffic
	for _, metric := range out.Metrics {
		out.HasData = out.HasData || metric.Total > 0 || metric.Recent > 0 || metric.Previous > 0
	}
	return out, tx.Commit()
}
