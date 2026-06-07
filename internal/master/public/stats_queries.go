package public

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s Store) loadMetric(ctx context.Context, kind, previousStart, start, end string, out *MetricStat) error {
	if err := s.loadMetricSummary(ctx, kind, previousStart, start, end, out); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, metricTrendSQL(kind), start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	series := map[string]int64{}
	for rows.Next() {
		var day string
		var value int64
		if err := rows.Scan(&day, &value); err != nil {
			return err
		}
		series[day] = value
	}
	out.Trend = fillSeries(start, end, series)
	return rows.Err()
}

func (s Store) loadMetricSummary(ctx context.Context, kind, previousStart, start, end string, out *MetricStat) error {
	totalSQL, dailySQL := metricSQL(kind)
	if err := s.DB.QueryRowContext(ctx, totalSQL).Scan(&out.Total); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, dailySQL, start, end).Scan(&out.Recent); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, dailySQL, previousStart, dateOffset(start, -1)).Scan(&out.Previous); err != nil {
		return err
	}
	out.TrendLabel = trendLabel(out.Recent, out.Previous)
	return nil
}

func (s Store) TopResources(ctx context.Context, start, end string, limit int) ([]ResourceRank, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.name, r.tag_name, a.file_name, a.architecture,
		COALESCE(a.system, '') AS system,
		COALESCE(SUM(das.authorization_count), 0) AS downloads
		FROM daily_asset_stats das
		JOIN assets a ON a.id = das.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		WHERE das.stat_day BETWEEN ? AND ?
		GROUP BY p.id, p.name, r.tag_name, a.file_name, a.architecture, COALESCE(a.system, '')
		ORDER BY downloads DESC, p.name, r.tag_name, a.architecture, a.file_name LIMIT ?`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceRank
	for rows.Next() {
		var item ResourceRank
		if err := rows.Scan(&item.ProjectName, &item.Version, &item.FileName,
			&item.Architecture, &item.System, &item.DownloadCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s Store) DailyTrends(ctx context.Context, start, end string) ([]DailyTrend, error) {
	views, err := queryDayMap(ctx, s.DB, `SELECT stat_day, page_views FROM daily_site_stats
		WHERE stat_day BETWEEN ? AND ?`, start, end)
	if err != nil {
		return nil, err
	}
	downloads, err := queryDayMap(ctx, s.DB, `SELECT stat_day, SUM(authorization_count)
		FROM daily_project_stats WHERE stat_day BETWEEN ? AND ? GROUP BY stat_day`, start, end)
	if err != nil {
		return nil, err
	}
	bytes, err := queryDayMap(ctx, s.DB, `SELECT stat_day, SUM(sent_bytes)
		FROM daily_project_stats WHERE stat_day BETWEEN ? AND ? GROUP BY stat_day`, start, end)
	if err != nil {
		return nil, err
	}
	return combineTrends(start, end, views, downloads, bytes), nil
}

func metricSQL(kind string) (string, string) {
	switch kind {
	case "views":
		return `SELECT COALESCE(SUM(page_views), 0) FROM daily_site_stats`,
			`SELECT COALESCE(SUM(page_views), 0) FROM daily_site_stats WHERE stat_day BETWEEN ? AND ?`
	case "downloads":
		return `SELECT COALESCE(SUM(authorization_count), 0) FROM daily_project_stats`,
			`SELECT COALESCE(SUM(authorization_count), 0) FROM daily_project_stats WHERE stat_day BETWEEN ? AND ?`
	default:
		return `SELECT COALESCE(SUM(sent_bytes), 0) FROM daily_project_stats`,
			`SELECT COALESCE(SUM(sent_bytes), 0) FROM daily_project_stats WHERE stat_day BETWEEN ? AND ?`
	}
}

func metricTrendSQL(kind string) string {
	total, _ := metricSQL(kind)
	field := strings.TrimPrefix(total, "SELECT COALESCE(SUM(")
	field = strings.Split(field, "), 0)")[0]
	table := "daily_project_stats"
	if kind == "views" {
		table = "daily_site_stats"
	}
	return fmt.Sprintf(`SELECT stat_day, COALESCE(SUM(%s), 0) FROM %s
		WHERE stat_day BETWEEN ? AND ? GROUP BY stat_day`, field, table)
}

func queryDayMap(ctx context.Context, db *sql.DB, query, start, end string) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx, query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var day string
		var value int64
		if err := rows.Scan(&day, &value); err != nil {
			return nil, err
		}
		out[day] = value
	}
	return out, rows.Err()
}

func dateOffset(day string, days int) string {
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
