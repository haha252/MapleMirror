package adminui

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

func slaWindow(db *sql.DB, r *http.Request, nodeID, name string, hours int) map[string]any {
	start := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339Nano)
	var total, ok int64
	_ = db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(total_samples), 0),
		COALESCE(SUM(ok_samples), 0)
		FROM node_availability_rollups WHERE node_id = ? AND bucket_start >= ?`,
		nodeID, start).Scan(&total, &ok)
	ratio := 0.0
	if total > 0 {
		ratio = float64(ok) / float64(total)
	}
	return map[string]any{"window": name, "availability_ratio": ratio,
		"sample_count": total, "insufficient_samples": total < 3}
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
