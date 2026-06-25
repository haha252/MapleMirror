package public

import (
	"context"
	"fmt"
)

func (s Store) Nodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, ''), COALESCE(public_download_base_url, '')
		FROM nodes WHERE state != 'disabled' ORDER BY public_name, id`)
	if err != nil {
		return nil, err
	}

	var out []NodeSummary
	for rows.Next() {
		var item NodeSummary
		var routingReady int
		if err := rows.Scan(&item.NodeID, &item.PublicName, &item.State, &routingReady, &item.LastHeartbeat, &item.PublicDownloadBaseURL); err != nil {
			return nil, err
		}
		item.RoutingReady = routingReady == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	nodeIDs := make([]string, 0, len(out))
	for _, item := range out {
		nodeIDs = append(nodeIDs, item.NodeID)
	}
	downloadStates, err := s.loadNodeDownloadReadyStates(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	slaTexts, err := s.loadNodeSLATexts(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	trafficTotals, err := s.loadNodeTrafficTotals(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		status := downloadStates[out[i].NodeID]
		out[i].DownloadReady = status.DownloadableCopies > 0 &&
			out[i].State != "disabled" && out[i].State != "offline" &&
			out[i].LastHeartbeat != "" && out[i].PublicDownloadBaseURL != "" &&
			!status.PublicProbeBlocked
		if !out[i].DownloadReady {
			info := s.nodeDownloadReadyInfo(ctx, out[i].NodeID, out[i].State, out[i].LastHeartbeat, out[i].PublicDownloadBaseURL, status)
			out[i].DownloadReadyReason = info.Summary
			out[i].DownloadReadyDetails = info.Detail
		}
		if !out[i].RoutingReady {
			info := s.nodeRoutingReadyInfo(ctx, out[i].NodeID, out[i].State, out[i].LastHeartbeat)
			out[i].RoutingReadyReason = info.Summary
			out[i].RoutingReadyDetails = info.Detail
		}
		out[i].SLA24H = slaTexts[out[i].NodeID].H24
		out[i].SLA7D = slaTexts[out[i].NodeID].D7
		out[i].SLA30D = slaTexts[out[i].NodeID].D30
		out[i].PressureRatio = "暂无"
		if s.Runtime != nil {
			if pressure, err := s.Runtime.LatestPressureReport(out[i].NodeID); err == nil {
				if ratio, ok := pressure["pressure_ratio"].(float64); ok {
					out[i].PressureRatio = percentText(ratio)
				}
			}
		}
		out[i].TotalSentBytes = trafficTotals[out[i].NodeID]
	}
	return out, nil
}

func percentText(value float64) string {
	return fmt.Sprintf("%.0f%%", value*100)
}
