package adminui

import (
	"context"

	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
)

func (s *Server) nodeSummaries(ctx context.Context, items []mastercontrol.NodeSummary) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"node_id": item.NodeID, "public_name": item.PublicName,
			"state": item.State, "connection_state": item.ConnectionState,
			"routing_ready": item.RoutingReady, "target_bandwidth_bps": item.TargetBandwidthBPS,
			"download_priority": item.DownloadPriority, "max_mirror_projects": item.MaxMirrorProjects,
			"project_assignment_mode": item.AssignmentMode,
			"last_heartbeat_at":       s.displayTime(item.LastHeartbeat),
		}
		if pressure, err := s.repo.LatestPressureReport(ctx, item.NodeID); err == nil {
			row["pressure"] = s.displayTimeMap(pressure)
		} else if heartbeat, err := s.repo.LatestHeartbeat(ctx, item.NodeID); err == nil {
			row["pressure"] = s.displayTimeMap(heartbeat)
		}
		out = append(out, row)
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
