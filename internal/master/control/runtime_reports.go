package control

import (
	"database/sql"
	"time"
)

func (s *RuntimeStore) LatestHeartbeat(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node := s.latest[nodeID]
	if !node.V2Status.ReportedAt.IsZero() {
		return v2StatusReport(node.V2Status), nil
	}
	item := node.Heartbeat
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"state": item.State, "pressure_ratio": item.PressureRatio,
		"active_downloads": item.ActiveDownloads, "free_bytes": item.FreeBytes,
		"target_bandwidth_bps": item.TargetBandwidth,
		"actual_bandwidth_bps": item.ActualBandwidth,
		"reported_at":          item.ReportedAt}, nil
}

func (s *RuntimeStore) LatestInventoryReport(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Inventory
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"revision": item.Revision, "complete": item.Complete,
		"item_count": item.ItemCount, "result": item.Result, "request_id": item.RequestID,
		"reported_at": item.Reported}, nil
}

func (s *RuntimeStore) LatestPressureReport(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	node := s.latest[nodeID]
	if !node.V2Status.ReportedAt.IsZero() {
		return v2StatusReport(node.V2Status), nil
	}
	item := node.Pressure
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"pressure_ratio": item.PressureRatio,
		"active_downloads": item.ActiveDownloads, "free_bytes": item.FreeBytes,
		"target_bandwidth_bps": item.TargetBandwidth,
		"actual_bandwidth_bps": item.ActualBandwidth,
		"request_id":           item.RequestID, "reported_at": item.ReportedAt}, nil
}

func v2StatusReport(item runtimeV2Status) map[string]any {
	status := item.Status
	ratio := float64(0)
	if status.TargetBandwidthBPS > 0 {
		ratio = float64(status.ActualBandwidthBPS) / float64(status.TargetBandwidthBPS)
	}
	free := int64(0)
	if status.AssetFS.Valid {
		free = status.AssetFS.AvailableBytes
	}
	return map[string]any{
		"state": status.Status, "pressure_ratio": ratio,
		"active_downloads":        status.PublicActiveDownloads,
		"public_active_downloads": status.PublicActiveDownloads,
		"swarm_active_uploads":    status.SwarmActiveUploads,
		"free_bytes":              free, "target_bandwidth_bps": status.TargetBandwidthBPS,
		"actual_bandwidth_bps": status.ActualBandwidthBPS,
		"asset_fs": map[string]any{"available_bytes": status.AssetFS.AvailableBytes,
			"total_bytes": status.AssetFS.TotalBytes, "reserved_bytes": status.AssetFS.ReservedBytes, "valid": status.AssetFS.Valid},
		"partial_fs": map[string]any{"available_bytes": status.PartialFS.AvailableBytes,
			"total_bytes": status.PartialFS.TotalBytes, "reserved_bytes": status.PartialFS.ReservedBytes, "valid": status.PartialFS.Valid},
		"reported_at": item.ReportedAt.Format(time.RFC3339Nano),
	}
}
