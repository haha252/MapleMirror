package adminui

import (
	"context"
	"time"
)

type overviewResponse struct {
	Stats    map[string]any    `json:"stats"`
	Nodes    []map[string]any  `json:"nodes"`
	NodeStat map[string]int    `json:"node_stat"`
	Scans    []map[string]any  `json:"scans"`
	User     map[string]string `json:"user"`
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
		COALESCE(authorization_count, 0),
		COALESCE(transfer_started_count, 0),
		COALESCE(sent_bytes, 0) FROM daily_public_stats
		WHERE stat_day = ?`, day).Scan(&auth, &started, &daily)
	_ = s.repo.DB.QueryRowContext(ctx, `SELECT COALESCE(t.sent_bytes, 0)
		FROM (SELECT 1) seed
		LEFT JOIN public_stat_totals t ON t.id = 'global'`).Scan(&total)
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
		out.NodeStat[connectionState]++
		if item.RoutingReady {
			out.NodeStat["routing_ready"]++
		}
		if connectionState != "offline" && connectionState != "disabled" {
			switch item.ControlProtocol {
			case "v2":
				out.NodeStat["control_v2"]++
			case "v1":
				out.NodeStat["control_v1"]++
			default:
				out.NodeStat["control_unknown"]++
			}
		}
	}
	out.Nodes = s.nodeSummaries(ctx, nodes)
	projectNames := s.projectNames(ctx)
	for _, item := range scans {
		out.Scans = append(out.Scans, map[string]any{
			"project_id": item.ProjectID, "project_name": projectNames[item.ProjectID], "enabled": item.Enabled,
			"last_scan_state": item.LastScanState, "next_scan_at": s.displayTime(item.NextScanAt),
			"last_error_message": item.LastErrorMessage,
			"updated_at":         s.displayTime(item.UpdatedAt),
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
