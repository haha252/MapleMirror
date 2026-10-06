package control

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

func (r Repository) logV2TaskWait(ctx context.Context, tx *sql.Tx, nodeID string, capacityBlocked bool) error {
	if r.Logger == nil {
		return nil
	}
	var asset string
	var size int64
	var manifest, seed, peer bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(t.asset_id,''),COALESCE(a.size_bytes,0),
		EXISTS(SELECT 1 FROM asset_piece_manifests m WHERE m.asset_id=t.asset_id),
		EXISTS(SELECT 1 FROM node_tasks s WHERE s.asset_id=t.asset_id AND s.id!=t.id AND s.task_type='asset_download'
			AND s.state IN ('sent','running') AND s.lease_expires_at>?),
		EXISTS(SELECT 1 `+syncPeerInventorySQL("t.asset_id", "t.node_id")+`)
		FROM node_tasks t LEFT JOIN assets a ON a.id=t.asset_id
		WHERE t.node_id=? AND t.state IN ('pending','retry_wait')`+eligibleSyncTaskSQL("t")+`
		ORDER BY t.updated_at LIMIT 1`, time.Now().UTC().Format(time.RFC3339Nano), nodeID).Scan(&asset, &size, &manifest, &seed, &peer)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	reason := "task_not_due"
	switch {
	case capacityBlocked:
		reason = "inflight_capacity"
		if latest, known := r.runtime().LatestV2Status(nodeID); known && latest.Status.SyncTaskSlotsAvailable <= 0 {
			reason = "slots_full"
		}
	case size > r.v2CapacityBudget(nodeID):
		reason = "disk_capacity"
	case !manifest && seed:
		reason = "manifest_initialization_in_progress"
	case !manifest && r.runtime().PeerOnly(nodeID) && !r.runtime().PeerBootstrap(nodeID):
		reason = "peer_bootstrap_not_supported"
	case !manifest && r.runtime().PeerOnly(nodeID) && !peer:
		reason = "no_verified_peer"
	}
	if r.runtime().allowV2WaitLog(nodeID, reason, time.Now()) {
		r.Logger.Info(ctx, "同步任务等待调度", slog.String("node_id", nodeID), slog.String("asset_id", asset), slog.String("reason", reason))
	}
	return nil
}

func (s *RuntimeStore) allowV2WaitLog(nodeID, reason string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.latest[nodeID]
	if n.V2WaitReason == reason && now.Sub(n.V2WaitLoggedAt) < time.Minute {
		return false
	}
	n.V2WaitReason, n.V2WaitLoggedAt = reason, now
	s.latest[nodeID] = n
	return true
}
