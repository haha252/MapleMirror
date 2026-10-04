package adminui

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

func slaWindows(ctx context.Context, db *sql.DB, nodeID string, now time.Time) ([]map[string]any, error) {
	start24 := now.Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	start7 := now.Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	start30 := now.Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	var totals, ok [3]int64
	err := db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN bucket_start >= ? THEN total_samples ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN bucket_start >= ? THEN ok_samples ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN bucket_start >= ? THEN total_samples ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN bucket_start >= ? THEN ok_samples ELSE 0 END),0),
		COALESCE(SUM(total_samples),0), COALESCE(SUM(ok_samples),0)
		FROM node_availability_rollups WHERE node_id = ? AND bucket_start >= ?`,
		start24, start24, start7, start7, nodeID, start30).
		Scan(&totals[0], &ok[0], &totals[1], &ok[1], &totals[2], &ok[2])
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, 3)
	for i, name := range []string{"24h", "7d", "30d"} {
		ratio := 0.0
		if totals[i] > 0 {
			ratio = float64(ok[i]) / float64(totals[i])
		}
		items = append(items, map[string]any{"window": name, "availability_ratio": ratio,
			"sample_count": totals[i], "insufficient_samples": totals[i] < 3})
	}
	return items, nil
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return "admin-ui"
}

type rowCounter interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
