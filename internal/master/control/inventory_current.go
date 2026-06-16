package control

import (
	"context"
	"database/sql"
)

func currentInventory(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID, assetID string) (string, string, int64, error) {
	var state, digest string
	var size int64
	err := tx.QueryRowContext(ctx, `SELECT state, local_digest_sha256, size_bytes
		FROM node_inventory WHERE node_id = ? AND asset_id = ?`, nodeID, assetID).
		Scan(&state, &digest, &size)
	if err == sql.ErrNoRows {
		return "", "", 0, nil
	}
	return state, digest, size, err
}
