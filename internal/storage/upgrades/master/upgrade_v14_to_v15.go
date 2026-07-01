package master

import (
	"context"
	"database/sql"
)

func V14ToV15(ctx context.Context, tx *sql.Tx) error {
	targetInventoryExists, err := tableExists(ctx, tx, "target_inventory")
	if err != nil {
		return err
	}
	if targetInventoryExists {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_target_inventory_asset_state_node
			ON target_inventory(asset_id, desired_state, node_id)`,
		); err != nil {
			return err
		}
	}
	nodeAssignmentsExists, err := tableExists(ctx, tx, "node_project_assignments")
	if err != nil {
		return err
	}
	if nodeAssignmentsExists {
		_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_node_project_assignments_project_assigned_node
			ON node_project_assignments(project_id, assigned, node_id)`)
	}
	return err
}
