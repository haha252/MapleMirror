package mirrorsync

import (
	"context"
	"database/sql"
)

func markUnacceptedReleaseAssetsRemoved(ctx context.Context, tx *sql.Tx, releaseID string, accepted map[int64]struct{}, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, github_asset_id FROM assets
		WHERE release_id = ? AND service_state IN ('candidate', 'pending', 'superseded')`, releaseID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var removed []string
	for rows.Next() {
		var assetID string
		var githubAssetID int64
		if err := rows.Scan(&assetID, &githubAssetID); err != nil {
			return err
		}
		if _, ok := accepted[githubAssetID]; !ok {
			removed = append(removed, assetID)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, assetID := range removed {
		if _, err := tx.ExecContext(ctx, `UPDATE assets SET service_state = 'removed'
			WHERE id = ?`, assetID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
			updated_at = ? WHERE asset_id = ?`, now, assetID); err != nil {
			return err
		}
	}
	return nil
}
