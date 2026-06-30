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
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(total_samples), 0),
		COALESCE(SUM(ok_samples), 0)
		FROM node_availability_rollups WHERE node_id = ? AND bucket_start >= ?`,
		nodeID, start).Scan(&total, &ok)
	if err != nil || total < 3 {
		return "统计样本不足"
	}
	return fmt.Sprintf("%.2f%%", float64(ok)*100/float64(total))
}

func (s Store) SampleNodeAvailability(ctx context.Context) error {
	now := timeNow()
	start := now.Truncate(5 * time.Minute).Format(time.RFC3339Nano)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, routing_ready,
		CASE WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at != '' THEN 1 ELSE 0 END
		FROM nodes WHERE state != 'disabled'`)
	if err != nil {
		return err
	}
	type sampleTarget struct {
		nodeID    string
		ready     int
		heartbeat int
	}
	var targets []sampleTarget
	for rows.Next() {
		var target sampleTarget
		if err := rows.Scan(&target.nodeID, &target.ready, &target.heartbeat); err != nil {
			return err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, target := range targets {
		if err := s.insertSample(ctx, target.nodeID, start, target.ready, target.heartbeat); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) insertSample(ctx context.Context, nodeID, start string, ready, heartbeat int) error {
	ok := 0
	if ready == 1 && heartbeat == 1 {
		ok = 1
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO node_availability_rollups
		(node_id, bucket_start, bucket_minutes, total_samples, ok_samples, updated_at)
		VALUES (?, ?, 5, 1, ?, ?)
		ON CONFLICT(node_id, bucket_start, bucket_minutes) DO UPDATE SET
		total_samples = total_samples + 1,
		ok_samples = ok_samples + excluded.ok_samples,
		updated_at = excluded.updated_at`,
		nodeID, start, ok, timeNow().Format(time.RFC3339Nano))
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}
