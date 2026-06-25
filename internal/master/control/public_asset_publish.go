package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/assetstate"
	"mirror-server/internal/master/assignment"
)

func publishVerifiedAsset(ctx context.Context, tx *sql.Tx, assetID, now string) ([]string, error) {
	projectID, err := assetstate.ReconcileAssetProject(ctx, tx, assetID)
	if err != nil || projectID == "" {
		return nil, err
	}
	if err := assetstate.RebuildTargetInventory(ctx, tx, projectID, now); err != nil {
		return nil, err
	}
	if err := assetstate.CancelObsoleteDownloadTasks(ctx, tx, projectID, now); err != nil {
		return nil, err
	}
	generated, err := assignment.GenerateProjectDeleteTasks(ctx, tx, projectID, now)
	if err != nil || generated == 0 {
		return nil, err
	}
	nodes, err := pendingDeleteTaskNodes(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func pendingDeleteTaskNodes(ctx context.Context, tx *sql.Tx, projectID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT t.node_id
		FROM node_tasks t JOIN assets a ON a.id = t.asset_id
		JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND t.task_type = 'asset_delete'
		AND t.state IN ('pending', 'retry_wait')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		nodes = append(nodes, nodeID)
	}
	return nodes, rows.Err()
}
