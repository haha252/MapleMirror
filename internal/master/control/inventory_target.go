package control

import (
	"context"
	"database/sql"
)

func inventoryTargetRequired(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID, assetID string) bool {
	var ok int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM target_inventory
		WHERE node_id = ? AND asset_id = ? AND desired_state = 'required'
	)`, nodeID, assetID).Scan(&ok)
	return err == nil && ok == 1
}
