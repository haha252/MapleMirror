package public

import (
	"context"
	"time"
)

func (s Store) loadMetricSummaries(ctx context.Context, previousStart, start, end string,
	views, downloads, traffic *MetricStat) error {
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(t.page_views, 0), COALESCE(t.authorization_count, 0),
		COALESCE(t.sent_bytes, 0)
		FROM (SELECT 1) seed
		LEFT JOIN public_stat_totals t ON t.id = 'global'`).
		Scan(&views.Total, &downloads.Total, &traffic.Total); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN page_views ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN page_views ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN sent_bytes ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN sent_bytes ELSE 0 END), 0)
		FROM daily_public_stats`,
		start, end, previousStart, dateOffset(start, -1),
		start, end, previousStart, dateOffset(start, -1),
		start, end, previousStart, dateOffset(start, -1)).
		Scan(&views.Recent, &views.Previous,
			&downloads.Recent, &downloads.Previous,
			&traffic.Recent, &traffic.Previous); err != nil {
		return err
	}
	views.TrendLabel = trendLabel(views.Recent, views.Previous)
	downloads.TrendLabel = trendLabel(downloads.Recent, downloads.Previous)
	traffic.TrendLabel = trendLabel(traffic.Recent, traffic.Previous)
	return nil
}

func (s Store) loadDownloadSourceSummaries(ctx context.Context, previousStart, start, end string,
	web, api *MetricStat) error {
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(t.web_authorization_count, 0),
		COALESCE(t.api_authorization_count, 0)
		FROM (SELECT 1) seed
		LEFT JOIN public_stat_totals t ON t.id = 'global'`).
		Scan(&web.Total, &api.Total); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN web_authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN web_authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN api_authorization_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN stat_day BETWEEN ? AND ? THEN api_authorization_count ELSE 0 END), 0)
		FROM daily_public_stats`,
		start, end, previousStart, dateOffset(start, -1),
		start, end, previousStart, dateOffset(start, -1)).
		Scan(&web.Recent, &web.Previous,
			&api.Recent, &api.Previous); err != nil {
		return err
	}
	web.TrendLabel = trendLabel(web.Recent, web.Previous)
	api.TrendLabel = trendLabel(api.Recent, api.Previous)
	return nil
}

func (s Store) TopResources(ctx context.Context, start, end string, limit int) ([]ResourceRank, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.name, r.tag_name, a.file_name, a.architecture,
		COALESCE(a.system, '') AS system,
		COALESCE(SUM(ast.authorization_count), 0) AS downloads,
		COALESCE(SUM(ast.web_authorization_count), 0) AS web_downloads,
		COALESCE(SUM(ast.api_authorization_count), 0) AS api_downloads
		FROM asset_stat_totals ast
		JOIN assets a ON a.id = ast.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		GROUP BY p.id, p.name, r.tag_name, a.file_name, a.architecture, COALESCE(a.system, '')
		ORDER BY downloads DESC, p.name, r.tag_name, a.architecture, a.file_name LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceRank
	for rows.Next() {
		var item ResourceRank
		if err := rows.Scan(&item.ProjectName, &item.Version, &item.FileName,
			&item.Architecture, &item.System, &item.DownloadCount,
			&item.WebDownloadCount, &item.APIDownloadCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s Store) DailyTrends(ctx context.Context, start, end string) ([]DailyTrend, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT stat_day,
		page_views, authorization_count, web_authorization_count,
		api_authorization_count, sent_bytes
		FROM daily_public_stats WHERE stat_day BETWEEN ? AND ?
		ORDER BY stat_day`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	views := map[string]int64{}
	downloads := map[string]int64{}
	webDownloads := map[string]int64{}
	apiDownloads := map[string]int64{}
	bytes := map[string]int64{}
	for rows.Next() {
		var day string
		var viewCount, downloadCount, webDownloadCount, apiDownloadCount, sentBytes int64
		if err := rows.Scan(&day, &viewCount, &downloadCount,
			&webDownloadCount, &apiDownloadCount, &sentBytes); err != nil {
			return nil, err
		}
		views[day] = viewCount
		downloads[day] = downloadCount
		webDownloads[day] = webDownloadCount
		apiDownloads[day] = apiDownloadCount
		bytes[day] = sentBytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return combineTrends(start, end, views, downloads, webDownloads, apiDownloads, bytes), nil
}

func dateOffset(day string, days int) string {
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
