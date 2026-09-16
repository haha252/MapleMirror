package master

import (
	"context"
	"database/sql"

	"mirror-server/internal/storage/upgrades"
)

func V20ToV21(ctx context.Context, tx *sql.Tx) error {
	tasksExist, err := allTablesExist(ctx, tx, []string{"node_tasks"})
	if err != nil {
		return err
	}
	hasAttempt := false
	if tasksExist {
		hasAttempt, err = upgrades.HasColumn(ctx, tx, "node_tasks", "attempt_id")
	}
	if err != nil {
		return err
	}
	if tasksExist && !hasAttempt {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE node_tasks ADD COLUMN attempt_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS asset_piece_manifests (
		id TEXT PRIMARY KEY,
		asset_id TEXT NOT NULL UNIQUE REFERENCES assets(id),
		asset_size INTEGER NOT NULL CHECK(asset_size >= 0),
		asset_sha256 TEXT NOT NULL,
		piece_layout_version INTEGER NOT NULL,
		piece_size INTEGER NOT NULL CHECK(piece_size > 0),
		piece_count INTEGER NOT NULL CHECK(piece_count > 0 AND piece_count <= 8192),
		piece_hash_blob BLOB NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('authoritative', 'conflict', 'disabled')),
		created_by_node_id TEXT REFERENCES nodes(id),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_asset_piece_manifests_asset
		ON asset_piece_manifests(asset_id, status);
	CREATE TABLE IF NOT EXISTS node_inventory_v2_segments (
		node_id TEXT NOT NULL REFERENCES nodes(id),
		revision INTEGER NOT NULL CHECK(revision > 0),
		segment INTEGER NOT NULL CHECK(segment >= 0),
		message_id TEXT NOT NULL UNIQUE,
		payload_hash TEXT NOT NULL,
		complete INTEGER NOT NULL CHECK(complete IN (0, 1)),
		item_count INTEGER NOT NULL CHECK(item_count >= 0),
		received_at TEXT NOT NULL,
		PRIMARY KEY(node_id, revision, segment)
	);
	CREATE INDEX IF NOT EXISTS idx_node_inventory_v2_segments_revision
		ON node_inventory_v2_segments(node_id, revision);`)
	return err
}
