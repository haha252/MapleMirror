package statbuffer

import (
	"context"
	"database/sql"
)

func flushPublic(ctx context.Context, tx *sql.Tx, day string, c Counter, now string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO daily_public_stats
		(stat_day, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stat_day) DO UPDATE SET
		page_views = page_views + excluded.page_views,
		authorization_count = authorization_count + excluded.authorization_count,
		web_authorization_count = web_authorization_count + excluded.web_authorization_count,
		api_authorization_count = api_authorization_count + excluded.api_authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		day, c.Views, c.Auth, c.WebAuth, c.APIAuth, c.Started, c.Bytes, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO public_stat_totals
		(id, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES ('global', ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		page_views = page_views + excluded.page_views,
		authorization_count = authorization_count + excluded.authorization_count,
		web_authorization_count = web_authorization_count + excluded.web_authorization_count,
		api_authorization_count = api_authorization_count + excluded.api_authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		c.Views, c.Auth, c.WebAuth, c.APIAuth, c.Started, c.Bytes, now)
	return err
}

func flushAsset(ctx context.Context, tx *sql.Tx, assetID string, c Counter, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO asset_stat_totals
		(asset_id, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(asset_id) DO UPDATE SET
		authorization_count = authorization_count + excluded.authorization_count,
		web_authorization_count = web_authorization_count + excluded.web_authorization_count,
		api_authorization_count = api_authorization_count + excluded.api_authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		assetID, c.Auth, c.WebAuth, c.APIAuth, c.Started, c.Bytes, now)
	return err
}

func flushProject(ctx context.Context, tx *sql.Tx, projectID string, c Counter, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_stat_totals
		(project_id, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		authorization_count = authorization_count + excluded.authorization_count,
		web_authorization_count = web_authorization_count + excluded.web_authorization_count,
		api_authorization_count = api_authorization_count + excluded.api_authorization_count,
		transfer_started_count = transfer_started_count + excluded.transfer_started_count,
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		projectID, c.Auth, c.WebAuth, c.APIAuth, c.Started, c.Bytes, now)
	return err
}

func flushNode(ctx context.Context, tx *sql.Tx, nodeID string, bytes int64, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_traffic_totals
		(node_id, sent_bytes, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
		sent_bytes = sent_bytes + excluded.sent_bytes,
		updated_at = excluded.updated_at`,
		nodeID, bytes, now)
	return err
}
