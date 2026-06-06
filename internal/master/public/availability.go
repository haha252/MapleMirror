package public

import (
	"context"
)

func (s Store) assetHasRoutableReplica(ctx context.Context, assetID string) bool {
	_, err := s.routableAsset(ctx, assetID)
	return err == nil
}

func (s Store) projectHasRoutableAsset(ctx context.Context, projectID string) bool {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id FROM assets a
		JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND r.selected = 1
		AND a.service_state = 'candidate'`, projectID)
	if err != nil {
		return false
	}
	var assetIDs []string
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			_ = rows.Close()
			return false
		}
		assetIDs = append(assetIDs, assetID)
	}
	if err := rows.Close(); err != nil {
		return false
	}
	if err := rows.Err(); err != nil {
		return false
	}
	for _, assetID := range assetIDs {
		if s.assetHasRoutableReplica(ctx, assetID) {
			return true
		}
	}
	return false
}
