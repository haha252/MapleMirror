package public

import (
	"context"
	"time"
)

func (s Store) loadMetricSummaries(ctx context.Context, previousStart, start, end string,
	views, downloads, traffic *MetricStat) error {
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(page_views), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN page_views ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN page_views ELSE 0 END), 0)
		FROM daily_site_stats`,
		start, end, previousStart, dateOffset(start, -1)).
		Scan(&views.Total, &views.Recent, &views.Previous); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(authorization_count), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN authorization_count ELSE 0 END), 0),
		COALESCE(SUM(sent_bytes), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN sent_bytes ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN sent_bytes ELSE 0 END), 0)
		FROM daily_project_stats`,
		start, end, previousStart, dateOffset(start, -1),
		start, end, previousStart, dateOffset(start, -1)).
		Scan(&downloads.Total, &downloads.Recent, &downloads.Previous,
			&traffic.Total, &traffic.Recent, &traffic.Previous); err != nil {
		return err
	}
	views.TrendLabel = trendLabel(views.Recent, views.Previous)
	downloads.TrendLabel = trendLabel(downloads.Recent, downloads.Previous)
	traffic.TrendLabel = trendLabel(traffic.Recent, traffic.Previous)
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
	rows, err := s.DB.QueryContext(ctx, `SELECT stat_day,
		COALESCE(SUM(page_views), 0),
		COALESCE(SUM(authorization_count), 0),
		COALESCE(SUM(sent_bytes), 0)
		FROM (
			SELECT stat_day, page_views, 0 AS authorization_count, 0 AS sent_bytes
			FROM daily_site_stats WHERE stat_day BETWEEN ? AND ?
			UNION ALL
			SELECT stat_day, 0 AS page_views, authorization_count, sent_bytes
			FROM daily_project_stats WHERE stat_day BETWEEN ? AND ?
		) GROUP BY stat_day`, start, end, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := map[string]int64{}
	downloads := map[string]int64{}
	bytes := map[string]int64{}
	for rows.Next() {
		var day string
		var viewCount, downloadCount, sentBytes int64
		if err := rows.Scan(&day, &viewCount, &downloadCount, &sentBytes); err != nil {
			return nil, err
		}
		views[day] = viewCount
		downloads[day] = downloadCount
		bytes[day] = sentBytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return combineTrends(start, end, views, downloads, bytes), nil
}

func dateOffset(day string, days int) string {
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
