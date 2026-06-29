package master

import (
	"context"
	"database/sql"
	"time"
)

func V11ToV12(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []string{
		`CREATE TABLE IF NOT EXISTS project_stat_totals (
			project_id TEXT PRIMARY KEY REFERENCES projects(id),
			authorization_count INTEGER NOT NULL DEFAULT 0,
			web_authorization_count INTEGER NOT NULL DEFAULT 0,
			api_authorization_count INTEGER NOT NULL DEFAULT 0,
			transfer_started_count INTEGER NOT NULL DEFAULT 0,
			sent_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_stat_totals_downloads
			ON project_stat_totals(authorization_count DESC, project_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return backfillProjectStatTotals(ctx, tx, now)
}

func backfillProjectStatTotals(ctx context.Context, tx *sql.Tx, now string) error {
	exists, err := tableExists(ctx, tx, "daily_project_stats")
	if err != nil || !exists {
		return err
	}
	exists, err = tableExists(ctx, tx, "projects")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_stat_totals
		(project_id, authorization_count, web_authorization_count, api_authorization_count,
		transfer_started_count, sent_bytes, updated_at)
		SELECT project_id, COALESCE(SUM(authorization_count), 0),
			COALESCE(SUM(web_authorization_count), 0), COALESCE(SUM(api_authorization_count), 0),
			COALESCE(SUM(transfer_started_count), 0), COALESCE(SUM(sent_bytes), 0), ?
		FROM daily_project_stats
		WHERE true
		GROUP BY project_id
		ON CONFLICT(project_id) DO UPDATE SET
			authorization_count = excluded.authorization_count,
			web_authorization_count = excluded.web_authorization_count,
			api_authorization_count = excluded.api_authorization_count,
			transfer_started_count = excluded.transfer_started_count,
			sent_bytes = excluded.sent_bytes,
			updated_at = excluded.updated_at`, now)
	return err
}
