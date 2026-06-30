package master

import (
	"context"
	"database/sql"
	"time"
)

func V13ToV14(ctx context.Context, tx *sql.Tx) error {
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	nodeRef := ""
	nodesExist, err := tableExists(ctx, tx, "nodes")
	if err != nil {
		return err
	}
	if nodesExist {
		nodeRef = " REFERENCES nodes(id)"
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS node_availability_rollups (
			node_id TEXT NOT NULL` + nodeRef + `,
			bucket_start TEXT NOT NULL,
			bucket_minutes INTEGER NOT NULL,
			total_samples INTEGER NOT NULL,
			ok_samples INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (node_id, bucket_start, bucket_minutes)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_node_availability_rollups_window
			ON node_availability_rollups(bucket_start, node_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := backfillAvailabilityRollups(ctx, tx, now); err != nil {
		return err
	}
	return cleanupV14History(ctx, tx, nowTime)
}

func backfillAvailabilityRollups(ctx context.Context, tx *sql.Tx, now string) error {
	exists, err := tableExists(ctx, tx, "node_availability_samples")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_availability_rollups
		(node_id, bucket_start, bucket_minutes, total_samples, ok_samples, updated_at)
		SELECT node_id, sample_start, 1, COUNT(*),
			COALESCE(SUM(CASE WHEN routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0), ?
		FROM node_availability_samples GROUP BY node_id, sample_start
		ON CONFLICT(node_id, bucket_start, bucket_minutes) DO UPDATE SET
			total_samples = excluded.total_samples,
			ok_samples = excluded.ok_samples,
			updated_at = excluded.updated_at`, now)
	return err
}

func cleanupV14History(ctx context.Context, tx *sql.Tx, now time.Time) error {
	ok, err := allTablesExist(ctx, tx, []string{"nodes", "assets", "download_authorizations"})
	if err != nil || !ok {
		return err
	}
	statements := []struct {
		tables []string
		sql    string
		args   []any
	}{
		{
			tables: []string{"node_availability_samples", "nodes"},
			sql:    `DELETE FROM node_availability_samples WHERE sample_start < ?`,
			args:   []any{now.AddDate(0, 0, -35).Format(time.RFC3339Nano)},
		},
		{
			tables: []string{"sync_scans"},
			sql: `WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY COALESCE(project_id, '') ORDER BY started_at DESC, id DESC
				) AS rn FROM sync_scans WHERE completed_at IS NOT NULL
			)
			DELETE FROM sync_scans WHERE id IN (SELECT id FROM ranked WHERE rn > 50)`,
		},
		{
			tables: []string{"traffic_event_dedupe", "node_traffic_cursors", "download_authorizations"},
			sql: `DELETE FROM traffic_event_dedupe
				WHERE EXISTS (
					SELECT 1 FROM node_traffic_cursors c
					WHERE c.node_id = traffic_event_dedupe.node_id
					AND traffic_event_dedupe.event_sequence <= c.last_event_sequence - 10000
				)`,
		},
		{
			tables: []string{"traffic_event_dedupe", "download_authorizations", "nodes"},
			sql: `DELETE FROM traffic_event_dedupe
				WHERE authorization_id IN (
					SELECT id FROM download_authorizations WHERE expires_at != '' AND expires_at < ?
				)`,
			args: []any{now.AddDate(0, 0, -30).Format(time.RFC3339Nano)},
		},
		{
			tables: []string{"traffic_reservations", "download_authorizations", "nodes", "assets"},
			sql: `DELETE FROM traffic_reservations
				WHERE authorization_id IN (
					SELECT id FROM download_authorizations WHERE expires_at != '' AND expires_at < ?
				)`,
			args: []any{now.AddDate(0, 0, -30).Format(time.RFC3339Nano)},
		},
	}
	for _, statement := range statements {
		ok, err := allTablesExist(ctx, tx, statement.tables)
		if err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	return nil
}
