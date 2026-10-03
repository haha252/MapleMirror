package control

import (
	"context"
	"time"
)

func (r Repository) SyncReset(ctx context.Context, nodeID, requestID, admin string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM nodes WHERE id=?`, nodeID).Scan(&state); err != nil {
		return err
	}
	if state != "disabled" {
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET routing_ready=0,updated_at=? WHERE id=?`, now, nodeID); err != nil {
			return err
		}
	}
	// Fence old results and leases; retain inventory and node-side partials.
	if _, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state='pending',attempt_id='',attempts=0,
		error_message=NULL,retry_after=NULL,lease_expires_at=NULL,completed_at=NULL,updated_at=?
		WHERE node_id=? AND task_type='asset_download' AND asset_id IN (
		 SELECT ti.asset_id FROM target_inventory ti
		 LEFT JOIN node_inventory ni ON ni.node_id=ti.node_id AND ni.asset_id=ti.asset_id
		 JOIN assets a ON a.id=ti.asset_id
		 WHERE ti.node_id=? AND ti.desired_state='required' AND
		 (ni.asset_id IS NULL OR ni.state!='verified' OR COALESCE(ni.local_digest_sha256,'')!=COALESCE(a.digest_sha256,''))
		)`, now, nodeID, nodeID); err != nil {
		return err
	}
	if err := seedNodeTargets(ctx, tx, nodeID, now); err != nil {
		return err
	}
	if _, err := clearSatisfiedDownloadTasks(ctx, tx, nodeID, now); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, "node.sync_reset", "node", nodeID, "success", requestID, "已重新调度缺失资产，保留已验证库存与分片", admin); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.runtime().NotifySyncTasks(nodeID)
	return nil
}
