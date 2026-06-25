package public

import (
	"context"
	"database/sql"
	"time"
)

type StatsDashboard struct {
	Today          string
	TotalViews     MetricStat
	TotalDownloads MetricStat
	TotalTraffic   MetricStat
	Resources      []ResourceRank
	Trend          []DailyTrend
}

type MetricStat struct {
	Total      int64
	Recent     int64
	Previous   int64
	Trend      []int64
	TrendLabel string
}

type ResourceRank struct {
	ProjectName   string
	Version       string
	FileName      string
	Architecture  string
	System        string
	DownloadCount int64
}

type DailyTrend struct {
	Day       string `json:"day"`
	Views     int64  `json:"views"`
	Downloads int64  `json:"downloads"`
	SentBytes int64  `json:"sent_bytes"`
}

func (s Store) StatsDashboard(ctx context.Context) (StatsDashboard, error) {
	today := statDay(timeNow(), s.Location)
	start, previousStart := dateOffset(today, -29), dateOffset(today, -59)
	var out StatsDashboard
	out.Today = today
	if err := s.loadMetricSummaries(ctx, previousStart, start, today,
		&out.TotalViews, &out.TotalDownloads, &out.TotalTraffic); err != nil {
		return out, err
	}
	resources, err := s.TopResources(ctx, start, today, 8)
	if err != nil {
		return out, err
	}
	out.Resources = resources
	trend, err := s.DailyTrends(ctx, start, today)
	if err != nil {
		return out, err
	}
	out.Trend = trend
	return out, nil
}

func (s Store) IncrementPageView(ctx context.Context) error {
	now := timeNow()
	day := statDay(now, s.Location)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO daily_site_stats
		(stat_day, page_views, updated_at) VALUES (?, 1, ?)
		ON CONFLICT(stat_day) DO UPDATE SET
		page_views = page_views + 1, updated_at = excluded.updated_at`,
		day, now.Format(time.RFC3339Nano))
	return err
}

func (s Store) AuthorizationBytes(ctx context.Context, id string) (int64, string, error) {
	var bytes int64
	var first sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(tr.settled_bytes, 0),
		COALESCE(da.first_transfer_at, '') FROM download_authorizations da
		LEFT JOIN traffic_reservations tr ON tr.authorization_id = da.id
		WHERE da.id = ?`, id).
		Scan(&bytes, &first)
	if !first.Valid {
		return bytes, "", err
	}
	return bytes, first.String, err
}

func upsertProjectStats(ctx context.Context, tx *sql.Tx, day, projectID string, auth, started, bytes int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(stat_day, project_id) DO UPDATE SET
		authorization_count = authorization_count + excluded.authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes`,
		day, projectID, auth, started, bytes)
	return err
}

func upsertAssetStats(ctx context.Context, tx *sql.Tx, day, assetID string, auth, started, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_asset_stats
		(stat_day, asset_id, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(stat_day, asset_id) DO UPDATE SET
		authorization_count = authorization_count + excluded.authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes, updated_at = excluded.updated_at`,
		day, assetID, auth, started, bytes, now)
	return err
}

func timeNow() time.Time {
	return time.Now().UTC()
}
