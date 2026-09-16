package assignment

import (
	"context"
	"database/sql"
)

func rebuildNodeTargets(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT ?, a.id, 'required', ?
		FROM node_project_assignments npa
		JOIN releases r ON r.project_id = npa.project_id AND r.selected = 1
		JOIN assets a ON a.release_id = r.id
			WHERE npa.node_id = ? AND npa.assigned = 1
			AND a.service_state IN ('candidate', 'pending', 'active', 'superseded')
			ON CONFLICT(node_id, asset_id) DO UPDATE SET
			desired_state = 'required', updated_at = excluded.updated_at
			WHERE target_inventory.desired_state != 'required'`,
		nodeID, now, nodeID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE desired_state != 'remove'
		AND (node_id, asset_id) IN (
		SELECT ti.node_id, ti.asset_id FROM target_inventory ti
		JOIN assets a ON a.id = ti.asset_id
		JOIN releases r ON r.id = a.release_id
			JOIN projects p ON p.id = r.project_id
			LEFT JOIN node_project_assignments npa ON npa.node_id = ti.node_id
				AND npa.project_id = p.id AND npa.assigned = 1
			WHERE ti.node_id = ? AND (p.enabled = 0
				OR a.service_state NOT IN ('candidate', 'pending', 'active', 'superseded') OR npa.project_id IS NULL
				OR (r.selected = 0 AND `+nodeHasSelectedProjectAssetsSQL("ti.node_id", "p.id")+`)))`,
		now, nodeID)
	return err
}

func RebuildProjectTargets(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT npa.node_id, a.id, 'required', ?
		FROM node_project_assignments npa
		JOIN releases r ON r.project_id = npa.project_id AND r.selected = 1
		JOIN assets a ON a.release_id = r.id
			WHERE npa.project_id = ? AND npa.assigned = 1
			AND a.service_state IN ('candidate', 'pending', 'active', 'superseded')
			ON CONFLICT(node_id, asset_id) DO UPDATE SET
			desired_state = 'required', updated_at = excluded.updated_at
			WHERE target_inventory.desired_state != 'required'`,
		now, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE desired_state != 'remove'
		AND (node_id, asset_id) IN (
		SELECT ti.node_id, ti.asset_id FROM target_inventory ti
			JOIN assets a ON a.id = ti.asset_id
			JOIN releases r ON r.id = a.release_id
			LEFT JOIN node_project_assignments npa ON npa.node_id = ti.node_id
				AND npa.project_id = r.project_id AND npa.assigned = 1
			WHERE r.project_id = ? AND (
				a.service_state NOT IN ('candidate', 'pending', 'active', 'superseded') OR npa.project_id IS NULL
				OR (r.selected = 0 AND `+nodeHasSelectedProjectAssetsSQL("ti.node_id", "r.project_id")+`)))`,
		now, projectID)
	return err
}

func nodeHasSelectedProjectAssetsSQL(nodeExpr, projectExpr string) string {
	// Retired-release targets are removed per node only after that node has a
	// verified copy of every asset in the currently selected release set. This
	// keeps the old local version available while a slow/offline node rolls
	// forward, instead of shrinking fleet redundancy as soon as the first node
	// finishes the new version.
	return `EXISTS (
		SELECT 1 FROM releases rr JOIN assets ra ON ra.release_id = rr.id
		WHERE rr.project_id = ` + projectExpr + ` AND rr.selected = 1
		AND ra.service_state IN ('candidate', 'pending', 'active', 'superseded')
	) AND NOT EXISTS (
		SELECT 1 FROM releases rr JOIN assets ra ON ra.release_id = rr.id
		LEFT JOIN node_inventory rni ON rni.node_id = ` + nodeExpr + ` AND rni.asset_id = ra.id
		WHERE rr.project_id = ` + projectExpr + ` AND rr.selected = 1
		AND ra.service_state IN ('candidate', 'pending', 'active', 'superseded')
		AND (rni.asset_id IS NULL OR rni.state != 'verified'
			OR rni.local_digest_sha256 != ra.digest_sha256 OR rni.size_bytes != ra.size_bytes)
	)`
}

func CancelObsoleteProjectTasks(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, obsoleteTaskSQL(`r.project_id = ?`), now, now, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, obsoleteDeleteTaskSQL(`r.project_id = ?`), now, now, projectID)
	return err
}

func CancelObsoleteNodeTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	_, err := tx.ExecContext(ctx, obsoleteTaskSQL(`node_tasks.node_id = ?`), now, now, nodeID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, obsoleteDeleteTaskSQL(`node_tasks.node_id = ?`), now, now, nodeID)
	return err
}

func obsoleteTaskSQL(extra string) string {
	return `UPDATE node_tasks SET state = 'obsolete',
		error_message = '资产已不在当前目标库存中', completed_at = ?,
		updated_at = ?, lease_expires_at = NULL
		WHERE task_type = 'asset_download'
		AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		AND asset_id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
			LEFT JOIN target_inventory ti ON ti.node_id = node_tasks.node_id
				AND ti.asset_id = a.id
			WHERE ` + extra + `
			AND (a.service_state NOT IN ('candidate', 'pending', 'active', 'superseded')
				OR ti.asset_id IS NULL OR ti.desired_state != 'required'))`
}

func obsoleteDeleteTaskSQL(extra string) string {
	return `UPDATE node_tasks SET state = 'obsolete',
		error_message = '删除目标已不在当前移除目标中', completed_at = ?,
		updated_at = ?, lease_expires_at = NULL
		WHERE task_type = 'asset_delete'
		AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		AND asset_id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
			LEFT JOIN target_inventory ti ON ti.node_id = node_tasks.node_id
				AND ti.asset_id = a.id
			LEFT JOIN node_inventory ni ON ni.node_id = node_tasks.node_id
				AND ni.asset_id = a.id
			WHERE ` + extra + `
			AND (ti.asset_id IS NULL OR ti.desired_state != 'remove'
				OR ni.asset_id IS NULL OR ni.state != 'verified'))`
}
