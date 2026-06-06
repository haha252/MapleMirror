package adminui

import (
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
)

func (s *Server) nodeSummaries(items []mastercontrol.NodeSummary) []mastercontrol.NodeSummary {
	out := make([]mastercontrol.NodeSummary, 0, len(items))
	for _, item := range items {
		item.LastHeartbeat = s.displayTime(item.LastHeartbeat)
		out = append(out, item)
	}
	return out
}

func (s *Server) syncStatusResponse(item mirrorsync.SyncStatus) map[string]any {
	return map[string]any{
		"node_id": item.NodeID, "connection_state": item.ConnectionState,
		"sync_phase": item.SyncPhase, "routing_ready": item.RoutingReady,
		"required_assets": item.RequiredAssets, "verified_assets": item.VerifiedAssets,
		"missing_assets": item.MissingAssets, "mismatched_assets": item.MismatchedAssets,
		"pending_tasks": item.PendingTasks, "sent_tasks": item.SentTasks,
		"running_tasks": item.RunningTasks, "retry_wait_tasks": item.RetryWaitTasks,
		"failed_tasks":              item.FailedTasks,
		"latest_inventory_revision": item.LatestInventoryRevision,
		"latest_inventory_complete": item.LatestInventoryComplete,
		"has_inventory_report":      item.HasInventoryReport,
		"active_control_session":    item.ActiveControlSession,
		"last_heartbeat_at":         s.displayTime(item.LastHeartbeatAt),
		"routing_ready_reason":      item.RoutingReadyReason,
		"routing_ready_detail":      item.RoutingReadyDetail,
	}
}
