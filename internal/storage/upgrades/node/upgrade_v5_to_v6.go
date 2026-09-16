package node

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V5ToV6(ctx context.Context, tx *sql.Tx) error {
	for _, item := range []struct{ table, column string }{
		{"local_sync_tasks", "attempt_id"},
		{"pending_sync_task_results", "attempt_id"},
	} {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, item.table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		has, err := upgrades.HasColumn(ctx, tx, item.table, item.column)
		if err != nil {
			return err
		}
		if !has {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+item.table+` ADD COLUMN attempt_id TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS swarm_partials (
		asset_id TEXT NOT NULL,
		manifest_id TEXT NOT NULL,
		partial_path TEXT NOT NULL,
		asset_size INTEGER NOT NULL CHECK(asset_size >= 0),
		piece_size INTEGER NOT NULL CHECK(piece_size > 0),
		piece_count INTEGER NOT NULL CHECK(piece_count > 0 AND piece_count <= 8192),
		verified_bitmap BLOB NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		last_access_at TEXT NOT NULL,
		PRIMARY KEY(asset_id, manifest_id)
	);
	CREATE INDEX IF NOT EXISTS idx_swarm_partials_access ON swarm_partials(last_access_at);
	CREATE TABLE IF NOT EXISTS pending_swarm_manifests (
		manifest_id TEXT PRIMARY KEY,
		asset_id TEXT NOT NULL,
		task_id TEXT NOT NULL,
		attempt_id TEXT NOT NULL,
		manifest_json BLOB NOT NULL,
		result_json BLOB NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_pending_swarm_manifests_task
		ON pending_swarm_manifests(task_id, attempt_id);`)
	return err
}
