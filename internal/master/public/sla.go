package public

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s Store) slaText(ctx context.Context, nodeID string, hours int) string {
	start := timeNow().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339Nano)
	var total, ok int64
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0)
		FROM node_availability_samples WHERE node_id = ? AND sample_start >= ?`,
		nodeID, start).Scan(&total, &ok)
	if err != nil || total < 3 {
		return "统计样本不足"
	}
	return fmt.Sprintf("%.2f%%", float64(ok)*100/float64(total))
}

func (s Store) SampleNodeAvailability(ctx context.Context) error {
	now := timeNow()
	start := now.Truncate(time.Minute).Format(time.RFC3339Nano)
	end := now.Truncate(time.Minute).Add(time.Minute).Format(time.RFC3339Nano)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, routing_ready,
		CASE WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at != '' THEN 1 ELSE 0 END
		FROM nodes WHERE state != 'disabled'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		var ready, heartbeat int
		if err := rows.Scan(&nodeID, &ready, &heartbeat); err != nil {
			return err
		}
		if err := s.insertSample(ctx, nodeID, start, end, ready, heartbeat); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s Store) insertSample(ctx context.Context, nodeID, start, end string, ready, heartbeat int) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO node_availability_samples
		(node_id, sample_start, sample_end, routable, heartbeat_ok)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(node_id, sample_start) DO UPDATE SET
		sample_end = excluded.sample_end, routable = excluded.routable,
		heartbeat_ok = excluded.heartbeat_ok`, nodeID, start, end, ready, heartbeat)
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}
