package adminui

import (
	"context"
	"time"
)

type overviewResponse struct {
	Stats    map[string]any    `json:"stats"`
	Nodes    []nodeItem        `json:"nodes"`
	NodeStat map[string]int    `json:"node_stat"`
	Scans    []map[string]any  `json:"scans"`
	User     map[string]string `json:"user"`
}

type nodeItem struct {
	NodeID          string `json:"node_id"`
	PublicName      string `json:"public_name"`
	State           string `json:"state"`
	ConnectionState string `json:"connection_state"`
	RoutingReady    bool   `json:"routing_ready"`
	LastHeartbeat   string `json:"last_heartbeat_at,omitempty"`
}

func (s *Server) overviewData(ctx context.Context) (overviewResponse, error) {
	nodes, err := s.repo.ListNodes(ctx)
	if err != nil {
		return overviewResponse{}, err
	}
	scans, err := s.syncStore.ListProjectScanStates(ctx)
	if err != nil {
		return overviewResponse{}, err
	}
	var auth, started, daily, total int64
	day := timeNowDay()
	_ = s.repo.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(authorization_count), 0),
		COALESCE(SUM(transfer_started_count), 0),
		COALESCE(SUM(sent_bytes), 0) FROM daily_project_stats
		WHERE stat_day = ?`, day).Scan(&auth, &started, &daily)
	_ = s.repo.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_bytes), 0)
		FROM traffic_events WHERE accounted_at IS NOT NULL`).Scan(&total)
	out := overviewResponse{
		Stats: map[string]any{"stat_day": day, "authorization_count": auth,
			"transfer_started_count": started, "daily_sent_bytes": daily,
			"total_sent_bytes": total},
		NodeStat: map[string]int{},
		User:     map[string]string{"name": ""},
	}
	for _, item := range nodes {
		connectionState := item.ConnectionState
		if connectionState == "" {
			connectionState = item.State
		}
		out.Nodes = append(out.Nodes, nodeItem{
			NodeID: item.NodeID, PublicName: item.PublicName,
			State: item.State, ConnectionState: connectionState,
			RoutingReady: item.RoutingReady, LastHeartbeat: item.LastHeartbeat,
		})
		out.NodeStat[connectionState]++
		if item.RoutingReady {
			out.NodeStat["routing_ready"]++
		}
	}
	for _, item := range scans {
		out.Scans = append(out.Scans, map[string]any{
			"project_id": item.ProjectID, "enabled": item.Enabled,
			"last_scan_state": item.LastScanState, "next_scan_at": item.NextScanAt,
			"last_error_message": item.LastErrorMessage,
			"updated_at":         item.UpdatedAt,
		})
	}
	return out, nil
}

func timeNowDay() string {
	return nowLocal().Format("2006-01-02")
}

func nowLocal() time.Time {
	return time.Now()
}
