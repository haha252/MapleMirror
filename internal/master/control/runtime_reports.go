package control

import "database/sql"

func (s *RuntimeStore) LatestHeartbeat(nodeID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].Heartbeat
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
	item := s.latest[nodeID].Pressure
	if !item.Valid {
		return nil, sql.ErrNoRows
	}
	return map[string]any{"pressure_ratio": item.PressureRatio,
		"active_downloads": item.ActiveDownloads, "free_bytes": item.FreeBytes,
		"target_bandwidth_bps": item.TargetBandwidth,
		"actual_bandwidth_bps": item.ActualBandwidth,
		"request_id":           item.RequestID, "reported_at": item.ReportedAt}, nil
}
