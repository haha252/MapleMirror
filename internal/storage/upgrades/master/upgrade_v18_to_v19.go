package master

import (
	"context"
	"database/sql"
)

func V18ToV19(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS project_developer_tokens (
			project_id TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			created_at TEXT NOT NULL,
			rotated_at TEXT NOT NULL,
			last_used_at TEXT
		);
		CREATE TABLE IF NOT EXISTS project_developer_api_daily_usage (
			project_id TEXT NOT NULL,
			usage_date TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0 CHECK(request_count >= 0),
			updated_at TEXT NOT NULL,
			PRIMARY KEY(project_id, usage_date)
		);
		CREATE INDEX IF NOT EXISTS idx_project_developer_tokens_hash
			ON project_developer_tokens(token_hash);
	`)
	return err
}
