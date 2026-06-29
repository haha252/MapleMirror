package master

import (
	"context"
	"database/sql"
	"time"
)

func V10ToV11(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE IF NOT EXISTS public_stat_totals (
			id TEXT PRIMARY KEY,
			page_views INTEGER NOT NULL DEFAULT 0,
			authorization_count INTEGER NOT NULL DEFAULT 0,
			web_authorization_count INTEGER NOT NULL DEFAULT 0,
			api_authorization_count INTEGER NOT NULL DEFAULT 0,
			transfer_started_count INTEGER NOT NULL DEFAULT 0,
			sent_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS daily_public_stats (
			stat_day TEXT PRIMARY KEY,
			page_views INTEGER NOT NULL DEFAULT 0,
			authorization_count INTEGER NOT NULL DEFAULT 0,
			web_authorization_count INTEGER NOT NULL DEFAULT 0,
			api_authorization_count INTEGER NOT NULL DEFAULT 0,
			transfer_started_count INTEGER NOT NULL DEFAULT 0,
			sent_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS asset_stat_totals (
			asset_id TEXT PRIMARY KEY REFERENCES assets(id),
			authorization_count INTEGER NOT NULL DEFAULT 0,
			web_authorization_count INTEGER NOT NULL DEFAULT 0,
			api_authorization_count INTEGER NOT NULL DEFAULT 0,
			transfer_started_count INTEGER NOT NULL DEFAULT 0,
			sent_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS node_traffic_totals (
			node_id TEXT PRIMARY KEY REFERENCES nodes(id),
			sent_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_stat_totals_downloads
			ON asset_stat_totals(authorization_count DESC, asset_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := createTrafficDedupeIndexes(ctx, tx); err != nil {
		return err
	}
	if err := backfillDailyPublicStats(ctx, tx, now); err != nil {
		return err
	}
	if err := backfillPublicStatTotals(ctx, tx, now); err != nil {
		return err
	}
	if err := backfillAssetStatTotals(ctx, tx, now); err != nil {
		return err
	}
	return backfillNodeTrafficTotals(ctx, tx, now)
}

func backfillDailyPublicStats(ctx context.Context, tx *sql.Tx, now string) error {
	exists, err := tableExists(ctx, tx, "daily_site_stats")
	if err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_public_stats
			(stat_day, page_views, authorization_count, web_authorization_count,
			api_authorization_count, transfer_started_count, sent_bytes, updated_at)
			SELECT stat_day, page_views, 0, 0, 0, 0, 0, ? FROM daily_site_stats
			WHERE true
			ON CONFLICT(stat_day) DO UPDATE SET
			page_views = excluded.page_views,
			updated_at = excluded.updated_at`, now); err != nil {
			return err
		}
	}
	exists, err = tableExists(ctx, tx, "daily_project_stats")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO daily_public_stats
		(stat_day, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		SELECT stat_day, 0, COALESCE(SUM(authorization_count), 0),
			COALESCE(SUM(web_authorization_count), 0),
			COALESCE(SUM(api_authorization_count), 0),
			COALESCE(SUM(transfer_started_count), 0),
			COALESCE(SUM(sent_bytes), 0), ?
		FROM daily_project_stats
		WHERE true
		GROUP BY stat_day
		ON CONFLICT(stat_day) DO UPDATE SET
			authorization_count = excluded.authorization_count,
			web_authorization_count = excluded.web_authorization_count,
			api_authorization_count = excluded.api_authorization_count,
			transfer_started_count = excluded.transfer_started_count,
			sent_bytes = excluded.sent_bytes,
			updated_at = excluded.updated_at`, now)
	return err
}

func backfillPublicStatTotals(ctx context.Context, tx *sql.Tx, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO public_stat_totals
		(id, page_views, authorization_count, web_authorization_count,
		api_authorization_count, transfer_started_count, sent_bytes, updated_at)
		SELECT 'global', COALESCE(SUM(page_views), 0), COALESCE(SUM(authorization_count), 0),
			COALESCE(SUM(web_authorization_count), 0), COALESCE(SUM(api_authorization_count), 0),
			COALESCE(SUM(transfer_started_count), 0), COALESCE(SUM(sent_bytes), 0), ?
		FROM daily_public_stats
		WHERE true
		ON CONFLICT(id) DO UPDATE SET
			page_views = excluded.page_views,
			authorization_count = excluded.authorization_count,
			web_authorization_count = excluded.web_authorization_count,
			api_authorization_count = excluded.api_authorization_count,
			transfer_started_count = excluded.transfer_started_count,
			sent_bytes = excluded.sent_bytes,
			updated_at = excluded.updated_at`, now)
	return err
}

func backfillAssetStatTotals(ctx context.Context, tx *sql.Tx, now string) error {
	exists, err := tableExists(ctx, tx, "daily_asset_stats")
	if err != nil || !exists {
		return err
	}
	exists, err = tableExists(ctx, tx, "assets")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO asset_stat_totals
		(asset_id, authorization_count, web_authorization_count, api_authorization_count,
		transfer_started_count, sent_bytes, updated_at)
		SELECT asset_id, COALESCE(SUM(authorization_count), 0),
			COALESCE(SUM(web_authorization_count), 0), COALESCE(SUM(api_authorization_count), 0),
			COALESCE(SUM(transfer_started_count), 0), COALESCE(SUM(sent_bytes), 0), ?
		FROM daily_asset_stats
		WHERE true
		GROUP BY asset_id
		ON CONFLICT(asset_id) DO UPDATE SET
			authorization_count = excluded.authorization_count,
			web_authorization_count = excluded.web_authorization_count,
			api_authorization_count = excluded.api_authorization_count,
			transfer_started_count = excluded.transfer_started_count,
			sent_bytes = excluded.sent_bytes,
			updated_at = excluded.updated_at`, now)
	return err
}

func backfillNodeTrafficTotals(ctx context.Context, tx *sql.Tx, now string) error {
	exists, err := tableExists(ctx, tx, "daily_node_traffic_stats")
	if err != nil || !exists {
		return err
	}
	exists, err = tableExists(ctx, tx, "nodes")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_traffic_totals
		(node_id, sent_bytes, updated_at)
		SELECT node_id, COALESCE(SUM(sent_bytes), 0), ?
		FROM daily_node_traffic_stats
		WHERE true
		GROUP BY node_id
		ON CONFLICT(node_id) DO UPDATE SET
			sent_bytes = excluded.sent_bytes,
			updated_at = excluded.updated_at`, now)
	return err
}

func createTrafficDedupeIndexes(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "traffic_event_dedupe")
	if err != nil || !exists {
		return err
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_traffic_event_dedupe_accounted
			ON traffic_event_dedupe(accounted_at DESC, node_id, event_sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_traffic_event_dedupe_authorization_accounted
			ON traffic_event_dedupe(authorization_id, accounted_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
