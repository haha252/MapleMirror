package control

import (
	"context"
	"database/sql"
	"sort"

	"mirror-server/internal/master/assetstate"
	"mirror-server/internal/master/assignment"
)

func publishVerifiedAsset(ctx context.Context, tx *sql.Tx, assetID, now string) ([]string, error) {
	projectID, err := assetstate.SelectedAssetProject(ctx, tx, assetID)
	if err != nil || projectID == "" {
		return nil, err
	}
	projects := map[string]struct{}{projectID: {}}
	return publishVerifiedProjects(ctx, tx, projects, now)
}

func publishVerifiedProjects(ctx context.Context, tx *sql.Tx, projects map[string]struct{}, now string) ([]string, error) {
	if len(projects) == 0 {
		return nil, nil
	}
	ids := sortedKeys(projects)
	nodeSet := map[string]struct{}{}
	for _, projectID := range ids {
		if err := assetstate.ReconcilePublicPaths(ctx, tx, projectID); err != nil {
			return nil, err
		}
		if err := assignment.ReconcileProjectNodes(ctx, tx, projectID, now); err != nil {
			return nil, err
		}
		if err := assignment.RebuildProjectTargets(ctx, tx, projectID, now); err != nil {
			return nil, err
		}
		if err := assetstate.CancelObsoleteDownloadTasks(ctx, tx, projectID, now); err != nil {
			return nil, err
		}
		generated, err := assignment.GenerateProjectDeleteTasks(ctx, tx, projectID, now)
		if err != nil {
			return nil, err
		}
		if generated == 0 {
			continue
		}
		nodes, err := pendingDeleteTaskNodes(ctx, tx, projectID)
		if err != nil {
			return nil, err
		}
		for _, nodeID := range nodes {
			nodeSet[nodeID] = struct{}{}
		}
	}
	return sortedKeys(nodeSet), nil
}

func recordVerifiedAssetProject(ctx context.Context, tx *sql.Tx, assetID string, projects map[string]struct{}) error {
	projectID, err := assetstate.SelectedAssetProject(ctx, tx, assetID)
	if err != nil || projectID == "" {
		return err
	}
	projects[projectID] = struct{}{}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
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
