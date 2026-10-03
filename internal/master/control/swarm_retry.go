package control

import (
	"context"
	"database/sql"
)

func (r Repository) retryableV2SwarmTask(ctx context.Context, tx *sql.Tx, nodeID, assetID string) bool {
	if r.runtime().ControlProtocol(nodeID) != "v2" {
		return false
	}
	var present int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM asset_piece_manifests
	 WHERE asset_id=? AND status='authoritative')`, assetID).Scan(&present)
	return err == nil && present == 1
}
