package master

import (
	"context"
	"database/sql"
	"time"
)

const (
	controlSessionKeepPerNode  = 1000
	inventoryReportKeepPerNode = 200
	syncScanKeepPerProject     = 100
	trafficDedupeKeepPerNode   = 5000
)

func V8ToV9(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC()
	statements := []struct {
		tables []string
		sql    string
		args   []any
	}{
		{
			tables: []string{"node_control_sessions", "nodes"},
			sql: `WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY node_id ORDER BY connected_at DESC, id DESC
				) AS rn
				FROM node_control_sessions
				WHERE disconnected_at IS NOT NULL
			)
			DELETE FROM node_control_sessions
			WHERE id IN (
				SELECT id FROM ranked WHERE rn > ?
			) AND connected_at < ?`,
			args: []any{
				controlSessionKeepPerNode,
				now.Add(-time.Hour).Format(time.RFC3339Nano),
			},
		},
		{
			tables: []string{"traffic_event_dedupe", "node_traffic_cursors", "download_authorizations", "nodes"},
			sql: `DELETE FROM traffic_event_dedupe
			WHERE accounted_at < ?
			AND EXISTS (
				SELECT 1 FROM node_traffic_cursors c
				WHERE c.node_id = traffic_event_dedupe.node_id
				AND traffic_event_dedupe.event_sequence < c.last_event_sequence - ?
			)`,
			args: []any{
				now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
				trafficDedupeKeepPerNode,
			},
		},
		{
			tables: []string{"node_inventory_reports", "nodes"},
			sql: `WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY node_id ORDER BY reported_at DESC, revision DESC, id DESC
				) AS rn
				FROM node_inventory_reports
			)
			DELETE FROM node_inventory_reports
			WHERE id IN (
				SELECT id FROM ranked WHERE rn > ?
			) AND reported_at < ?`,
			args: []any{
				inventoryReportKeepPerNode,
				now.AddDate(0, 0, -7).Format(time.RFC3339Nano),
			},
		},
		{
			tables: []string{"sync_scans"},
			sql: `WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY COALESCE(project_id, '') ORDER BY started_at DESC, id DESC
				) AS rn
				FROM sync_scans
				WHERE completed_at IS NOT NULL
			)
			DELETE FROM sync_scans
			WHERE id IN (
				SELECT id FROM ranked WHERE rn > ?
			) AND completed_at < ?`,
			args: []any{
				syncScanKeepPerProject,
				now.AddDate(0, 0, -14).Format(time.RFC3339Nano),
			},
		},
		{
			tables: []string{"node_availability_samples", "nodes"},
			sql:    `DELETE FROM node_availability_samples WHERE sample_start < ?`,
			args:   []any{now.AddDate(0, 0, -35).Format(time.RFC3339Nano)},
		},
		{
			tables: []string{"traffic_event_dedupe", "download_authorizations", "nodes"},
			sql: `DELETE FROM traffic_event_dedupe
			WHERE authorization_id IN (
				SELECT id FROM download_authorizations
				WHERE expires_at != '' AND expires_at < ?
			)`,
			args: []any{now.AddDate(0, 0, -30).Format(time.RFC3339Nano)},
		},
		{
			tables: []string{"traffic_reservations", "download_authorizations"},
			sql: `DELETE FROM traffic_reservations
			WHERE authorization_id IN (
				SELECT id FROM download_authorizations
				WHERE expires_at != '' AND expires_at < ?
			)`,
			args: []any{now.AddDate(0, 0, -30).Format(time.RFC3339Nano)},
		},
		{
			tables: []string{"download_authorizations", "assets", "nodes"},
			sql: `DELETE FROM download_authorizations
			WHERE expires_at != '' AND expires_at < ?`,
			args: []any{now.AddDate(0, 0, -30).Format(time.RFC3339Nano)},
		},
	}
	for _, statement := range statements {
		ok, err := allTablesExist(ctx, tx, statement.tables)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func allTablesExist(ctx context.Context, tx *sql.Tx, tables []string) (bool, error) {
	for _, table := range tables {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
			WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return false, err
		}
		if count == 0 {
			return false, nil
		}
	}
	return true, nil
}
