package public

import (
	"context"
	"database/sql"
)

type statsCounterExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func addPublicStatCounters(ctx context.Context, exec statsCounterExec, day string,
	views, auth, webAuth, apiAuth, started, bytes int64, now string) error {
	if err := addDailyPublicStatCounters(ctx, exec, day, views, auth, webAuth, apiAuth, started, bytes, now); err != nil {
		return err
	}
	return addPublicStatTotals(ctx, exec, views, auth, webAuth, apiAuth, started, bytes, now)
}

func addDailyPublicStatCounters(ctx context.Context, exec statsCounterExec, day string,
	views, auth, webAuth, apiAuth, started, bytes int64, now string) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO daily_public_stats
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
		day, views, auth, webAuth, apiAuth, started, bytes, now)
	return err
}

func addPublicStatTotals(ctx context.Context, exec statsCounterExec,
	views, auth, webAuth, apiAuth, started, bytes int64, now string) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO public_stat_totals
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
		views, auth, webAuth, apiAuth, started, bytes, now)
	return err
}

func addAssetStatCounters(ctx context.Context, exec statsCounterExec, assetID string,
	auth, webAuth, apiAuth, started, bytes int64, now string) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO asset_stat_totals
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
		assetID, auth, webAuth, apiAuth, started, bytes, now)
	return err
}
