package master

import (
	"context"
	"database/sql"
)

func V7ToV8(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS traffic_event_dedupe (
			node_id TEXT NOT NULL REFERENCES nodes(id),
			event_sequence INTEGER NOT NULL,
			authorization_id TEXT NOT NULL REFERENCES download_authorizations(id),
			event_hash TEXT NOT NULL,
			accounted_at TEXT NOT NULL,
			PRIMARY KEY(node_id, event_sequence)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_traffic_event_dedupe_authorization
			ON traffic_event_dedupe(authorization_id)`,
		`CREATE TABLE IF NOT EXISTS node_traffic_cursors (
			node_id TEXT PRIMARY KEY REFERENCES nodes(id),
			last_event_sequence INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS archive_migrations (
			name TEXT PRIMARY KEY,
			completed_at TEXT NOT NULL,
			details_json TEXT NOT NULL DEFAULT '{}'
		)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
