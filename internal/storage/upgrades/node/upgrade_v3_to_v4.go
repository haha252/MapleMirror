package node

import (
	"context"
	"database/sql"
)

func V3ToV4(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS local_authorizations (
		authorization_id TEXT PRIMARY KEY,
		asset_id TEXT NOT NULL,
		node_id TEXT NOT NULL,
		issued_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		first_seen_at TEXT,
		last_activity_at TEXT,
		status TEXT NOT NULL,
		reason TEXT,
		reported_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_local_authorizations_report
		ON local_authorizations(reported_at, updated_at)`)
	return err
}
