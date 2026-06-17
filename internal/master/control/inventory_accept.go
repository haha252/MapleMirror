package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/protocol"
)

func acceptInventoryItem(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, nodeID string, item protocol.InventoryItem, now string) (inventoryAcceptResult, error) {
	result := inventoryAcceptResult{
		AssetID:     item.AssetID,
		LocalDigest: item.DigestSHA256,
		LocalSize:   item.SizeBytes,
	}
	var expectedDigest string
	var expectedSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, item.AssetID).Scan(&expectedDigest, &expectedSize)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !inventoryTargetRequired(ctx, tx, nodeID, item.AssetID) {
		return result, nil
	}
	result.ExpectedDigest = expectedDigest
	result.ExpectedSize = expectedSize
	result.PublicAsset = publicCandidateAsset(ctx, tx, item.AssetID)
	localDigest := item.DigestSHA256
	localSize := item.SizeBytes
	previousState, previousDigest, previousSize, err := currentInventory(ctx, tx, nodeID, item.AssetID)
	if err != nil {
		return result, err
	}
	state := "verified"
	switch item.LocalState {
	case "missing":
		state = "missing"
		if localDigest == "" {
			localSize = 0
		}
	case "mismatch":
		state = "mismatch"
	default:
		if previousState == "stale" && localDigest == previousDigest && localSize == previousSize {
			state = "stale"
		} else if localDigest != expectedDigest || localSize != expectedSize {
			state = "mismatch"
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`,
		nodeID, item.AssetID, localDigest, localSize, now, state)
	result.State = state
	result.LocalDigest = localDigest
	result.LocalSize = localSize
	return result, err
}
