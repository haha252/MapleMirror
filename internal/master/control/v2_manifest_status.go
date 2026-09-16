package control

import (
	"context"
	"database/sql"
)

func (r Repository) v2ManifestStatus(ctx context.Context, assetID string) (string, bool, error) {
	var status string
	err := r.DB.QueryRowContext(ctx, `SELECT status FROM asset_piece_manifests WHERE asset_id=?`, assetID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}
