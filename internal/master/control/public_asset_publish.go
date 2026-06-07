package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assetstate"
)

func publishVerifiedAsset(ctx context.Context, tx *sql.Tx, assetID, now string) error {
	projectID, err := assetstate.ReconcileAssetProject(ctx, tx, assetID)
	if err != nil || projectID == "" {
		return err
	}
	if err := assetstate.RebuildTargetInventory(ctx, tx, projectID, now); err != nil {
		return err
	}
	if err := assetstate.CancelObsoleteDownloadTasks(ctx, tx, projectID, now); err != nil {
		return err
	}
	return nil
}
