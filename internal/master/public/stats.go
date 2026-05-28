package public

import (
	"context"
	"database/sql"
	"time"
)

type StatsOverview struct {
	StatDay              string `json:"stat_day"`
	AuthorizationCount   int64  `json:"authorization_count"`
	TransferStartedCount int64  `json:"transfer_started_count"`
	DailySentBytes       int64  `json:"daily_sent_bytes"`
	TotalSentBytes       int64  `json:"total_sent_bytes"`
}

type ProjectStat struct {
	ProjectID            string `json:"project_id"`
	AuthorizationCount   int64  `json:"authorization_count"`
	TransferStartedCount int64  `json:"transfer_started_count"`
	SentBytes            int64  `json:"sent_bytes"`
}

func (s Store) StatsOverview(ctx context.Context) (StatsOverview, error) {
	day := statDay(timeNow(), s.Location)
	var out StatsOverview
	out.StatDay = day
	err := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(authorization_count), 0),
		COALESCE(SUM(transfer_started_count), 0),
		COALESCE(SUM(sent_bytes), 0)
		FROM daily_project_stats WHERE stat_day = ?`, day).
		Scan(&out.AuthorizationCount, &out.TransferStartedCount, &out.DailySentBytes)
	if err != nil {
		return out, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_bytes), 0)
		FROM traffic_events WHERE accounted_at IS NOT NULL`).Scan(&out.TotalSentBytes)
	return out, err
}

func (s Store) ProjectStats(ctx context.Context, day string) ([]ProjectStat, error) {
	if day == "" {
		day = statDay(timeNow(), s.Location)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id, authorization_count,
		transfer_started_count, sent_bytes FROM daily_project_stats
		WHERE stat_day = ? ORDER BY project_id`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectStat
	for rows.Next() {
		var item ProjectStat
		if err := rows.Scan(&item.ProjectID, &item.AuthorizationCount,
			&item.TransferStartedCount, &item.SentBytes); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s Store) AuthorizationBytes(ctx context.Context, id string) (int64, string, error) {
	var bytes int64
	var first sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(te.sent_bytes), 0),
		COALESCE(da.first_transfer_at, '') FROM download_authorizations da
		LEFT JOIN traffic_events te ON te.authorization_id = da.id
		AND te.accounted_at IS NOT NULL WHERE da.id = ? GROUP BY da.id`, id).
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

func timeNow() time.Time {
	return time.Now().UTC()
}
