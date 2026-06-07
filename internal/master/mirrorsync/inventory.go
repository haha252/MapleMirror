package mirrorsync

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assetstate"
	"mirror-server/internal/master/assignment"
)

func rebuildTargetInventory(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	return assetstate.RebuildTargetInventory(ctx, tx, projectID, now)
}

func cancelObsoleteDownloadTasks(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	return assetstate.CancelObsoleteDownloadTasks(ctx, tx, projectID, now)
}

func generateTasks(ctx context.Context, tx *sql.Tx, now string) (int, error) {
	return assignment.GenerateMissingTasks(ctx, tx, now)
}
