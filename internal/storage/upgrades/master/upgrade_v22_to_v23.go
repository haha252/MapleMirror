package master

import (
	"context"
	"database/sql"
)

func V22ToV23(ctx context.Context, tx *sql.Tx) error {
	indexes := []struct{ table, query string }{
		{"download_authorizations", `CREATE INDEX IF NOT EXISTS idx_download_authorizations_pending_delivery
			ON download_authorizations(node_id, issued_at, id)
			WHERE token_hash != '' AND delivered_at = '' AND status IN ('issued', 'active')`},
		{"target_inventory", `CREATE INDEX IF NOT EXISTS idx_target_inventory_required_counts
			ON target_inventory(node_id, asset_id) WHERE desired_state = 'required'`},
		{"node_inventory", `CREATE INDEX IF NOT EXISTS idx_node_inventory_live_counts
			ON node_inventory(node_id, state) WHERE state IN ('verified', 'mismatch')`},
		{"node_tasks", `CREATE INDEX IF NOT EXISTS idx_node_tasks_live_counts
			ON node_tasks(node_id, state) WHERE state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`},
		{"node_availability_rollups", `CREATE INDEX IF NOT EXISTS idx_node_availability_rollups_counts
			ON node_availability_rollups(node_id, bucket_start, total_samples, ok_samples)`},
	}
	for _, index := range indexes {
		exists, err := tableExists(ctx, tx, index.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, index.query); err != nil {
			return err
		}
	}
	return nil
}
