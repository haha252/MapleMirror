package control

import (
	"context"
	"database/sql"
	"strings"

	"mirror-server/internal/protocol"
)

// Bound SQL parameters even on SQLite builds with a 999-variable limit.
const inventoryLookupBatchSize = 250
const inventoryWriteBatchSize = 150

type inventoryItemContext struct {
	result        inventoryAcceptResult
	projectID     string
	previousState string
	previousHash  string
	previousSize  int64
}

// acceptInventoryItems keeps the segment atomic while avoiding per-item metadata
// queries. Process in report order so duplicate items and quarantine retain the
// same behavior as sequential acceptance.
func (r Repository) acceptInventoryItems(ctx context.Context, tx *sql.Tx, session Session,
	items []protocol.InventoryItem, now string) (map[string]struct{}, bool, error) {
	metadata, err := loadInventoryItemContexts(ctx, tx, session.NodeID, items)
	if err != nil {
		return nil, false, err
	}
	projects := map[string]struct{}{}
	pending := make([]inventoryAcceptResult, 0, inventoryWriteBatchSize)
	for _, item := range items {
		meta, exists := metadata[item.AssetID]
		if !exists {
			continue
		}
		result := resolveInventoryItem(meta.result, item, meta.previousState, meta.previousHash, meta.previousSize)
		meta.previousState, meta.previousHash, meta.previousSize = result.State, result.LocalDigest, result.LocalSize
		metadata[item.AssetID] = meta
		pending = append(pending, result)
		quarantine := result.TargetRequired && result.PublicAsset && result.State == "mismatch"
		if len(pending) == inventoryWriteBatchSize || quarantine {
			if err := writeInventoryItems(ctx, tx, session.NodeID, pending, now); err != nil {
				return nil, false, err
			}
			pending = pending[:0]
		}
		if quarantine {
			if err := r.quarantineNodeForPublicAssetMismatch(ctx, tx, session, result, now); err != nil {
				return nil, false, err
			}
			return projects, true, nil
		}
		if result.TargetRequired && result.State == "verified" && meta.projectID != "" {
			projects[meta.projectID] = struct{}{}
		}
	}
	if err := writeInventoryItems(ctx, tx, session.NodeID, pending, now); err != nil {
		return nil, false, err
	}
	return projects, false, nil
}

func loadInventoryItemContexts(ctx context.Context, tx *sql.Tx, nodeID string,
	items []protocol.InventoryItem) (map[string]inventoryItemContext, error) {
	metadata := make(map[string]inventoryItemContext, len(items))
	for start := 0; start < len(items); start += inventoryLookupBatchSize {
		end := min(start+inventoryLookupBatchSize, len(items))
		args := []any{nodeID, nodeID}
		for _, item := range items[start:end] {
			args = append(args, item.AssetID)
		}
		rows, err := tx.QueryContext(ctx, `SELECT a.id, a.digest_sha256, a.size_bytes,
			COALESCE(t.desired_state = 'required', 0),
			(a.service_state = 'candidate' AND r.selected = 1 AND p.enabled = 1),
			CASE WHEN r.selected = 1 THEN r.project_id ELSE '' END,
			COALESCE(ni.state, ''), COALESCE(ni.local_digest_sha256, ''), COALESCE(ni.size_bytes, 0)
			FROM assets a JOIN releases r ON r.id = a.release_id
			JOIN projects p ON p.id = r.project_id
			LEFT JOIN target_inventory t ON t.node_id = ? AND t.asset_id = a.id
			LEFT JOIN node_inventory ni ON ni.node_id = ? AND ni.asset_id = a.id
			WHERE a.id IN (`+strings.TrimSuffix(strings.Repeat("?,", end-start), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var meta inventoryItemContext
			if err := rows.Scan(&meta.result.AssetID, &meta.result.ExpectedDigest, &meta.result.ExpectedSize,
				&meta.result.TargetRequired, &meta.result.PublicAsset, &meta.projectID,
				&meta.previousState, &meta.previousHash, &meta.previousSize); err != nil {
				_ = rows.Close()
				return nil, err
			}
			meta.result.PublicAsset = meta.result.TargetRequired && meta.result.PublicAsset
			metadata[meta.result.AssetID] = meta
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return metadata, nil
}

func writeInventoryItems(ctx context.Context, tx *sql.Tx, nodeID string,
	items []inventoryAcceptResult, now string) error {
	if len(items) == 0 {
		return nil
	}
	args := make([]any, 0, len(items)*6)
	for _, item := range items {
		args = append(args, nodeID, item.AssetID, item.LocalDigest, item.LocalSize, now, item.State)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state) VALUES `+
		strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?),", len(items)), ",")+`
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, verified_at = excluded.verified_at,
		state = excluded.state`, args...)
	return err
}
