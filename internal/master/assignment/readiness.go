package assignment

import (
	"context"
	"database/sql"
)

// Readiness is derived from current targets and unfinished work, not a cached
// inventory report. Keep this update in the transaction which changes targets.
func ReconcileReadiness(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	return reconcileReadiness(ctx, tx, nodeID, "", now, true)
}

// Task/target maintenance may revoke readiness, but a heartbeat must never
// promote a node before the inventory/result confirmation path runs.
func InvalidateReadiness(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	return reconcileReadiness(ctx, tx, nodeID, "", now, false)
}

func reconcileReadiness(ctx context.Context, tx *sql.Tx, nodeID, projectID, now string, promote bool) error {
	_, err := tx.ExecContext(ctx, `WITH readiness AS MATERIALIZED (
		SELECT n.id, n.routing_ready AS previous, CASE WHEN
			n.state NOT IN ('disabled','offline') AND COALESCE(n.last_heartbeat_at,'')!=''
			AND NOT EXISTS(SELECT 1 FROM target_inventory ti
				LEFT JOIN node_inventory ni ON ni.node_id=ti.node_id AND ni.asset_id=ti.asset_id
				LEFT JOIN assets a ON a.id=ti.asset_id
				WHERE ti.node_id=n.id AND ti.desired_state='required'
				AND (ni.asset_id IS NULL OR ni.state!='verified' OR ni.local_digest_sha256!=a.digest_sha256 OR ni.size_bytes!=a.size_bytes))
			AND NOT EXISTS(SELECT 1 FROM node_tasks t WHERE t.node_id=n.id
				AND t.state IN ('pending','sent','running','retry_wait','failed'))
			THEN 1 ELSE 0 END AS ready
		FROM nodes n WHERE (?='' AND ?='') OR n.id=? OR (?!='' AND EXISTS(
			SELECT 1 FROM target_inventory ti JOIN assets a ON a.id=ti.asset_id
			JOIN releases r ON r.id=a.release_id WHERE ti.node_id=n.id AND r.project_id=?))
	)
	UPDATE nodes SET routing_ready=(SELECT ready FROM readiness WHERE id=nodes.id), updated_at=?
	WHERE id IN (SELECT id FROM readiness WHERE previous!=ready AND (? OR ready=0))`, nodeID, projectID, nodeID, projectID, projectID, now, promote)
	return err
}
