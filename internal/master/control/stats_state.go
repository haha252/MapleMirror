package control

import (
	"context"
	"database/sql"
)

func addPublicTrafficCounters(ctx context.Context, tx *sql.Tx, day string,
	started, bytes int64, now string) error {
	if err := addDailyPublicTrafficCounters(ctx, tx, day, started, bytes, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO public_stat_totals
		(id, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('global', 0, 0, 0, 0, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		started, bytes, now)
	return err
}

func addDailyPublicTrafficCounters(ctx context.Context, tx *sql.Tx, day string,
	started, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_public_stats
		(stat_day, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, 0, 0, 0, 0, ?, ?, ?)
		ON CONFLICT(stat_day) DO UPDATE SET
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		day, started, bytes, now)
	return err
}

func addAssetTrafficCounters(ctx context.Context, tx *sql.Tx, assetID string,
	started, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO asset_stat_totals
		(asset_id, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, 0, 0, 0, ?, ?, ?)
		ON CONFLICT(asset_id) DO UPDATE SET
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		assetID, started, bytes, now)
	return err
}

func addProjectTrafficCounters(ctx context.Context, tx *sql.Tx, projectID string,
	started, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_stat_totals
		(project_id, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, 0, 0, 0, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		projectID, started, bytes, now)
	return err
}

func addNodeTrafficCounters(ctx context.Context, tx *sql.Tx, nodeID string,
	bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_traffic_totals
		(node_id, sent_bytes, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		nodeID, bytes, now)
	return err
}
