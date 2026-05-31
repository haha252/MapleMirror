package control

import (
	"context"
	"database/sql"
)

func upsertTrafficDay(ctx context.Context, tx *sql.Tx, day, kind, key string, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_traffic_stats
		(stat_day, scope_kind, scope_key, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(stat_day, scope_kind, scope_key) DO UPDATE SET
		sent_bytes = sent_bytes + excluded.sent_bytes, updated_at = excluded.updated_at`,
		day, kind, key, bytes, now)
	return err
}

func upsertProjectTraffic(ctx context.Context, tx *sql.Tx, info authAccounting, bytes int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_project_stats
		(stat_day, project_id, authorization_count, transfer_started_count, sent_bytes)
		VALUES (?, ?, 0, ?, ?)
		ON CONFLICT(stat_day, project_id) DO UPDATE SET
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes`,
		info.Day, info.ProjectID, info.StartedIncrement, bytes)
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

func upsertNodeTraffic(ctx context.Context, tx *sql.Tx, day, nodeID string, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_node_traffic_stats
		(stat_day, node_id, sent_bytes, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(stat_day, node_id) DO UPDATE SET
		sent_bytes = sent_bytes + excluded.sent_bytes, updated_at = excluded.updated_at`,
		day, nodeID, bytes, now)
	return err
}
