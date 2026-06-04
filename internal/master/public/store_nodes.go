package public

import "context"

func (s Store) Nodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, ''), COALESCE(public_download_base_url, '')
		FROM nodes ORDER BY public_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
	for i := range out {
		status, err := s.loadNodeDownloadReadyState(ctx, out[i].NodeID)
		if err != nil {
			return nil, err
		}
		out[i].DownloadReady = status.DownloadableCopies > 0 &&
			out[i].State != "disabled" && out[i].State != "offline" &&
			out[i].LastHeartbeat != "" && out[i].PublicDownloadBaseURL != ""
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
		out[i].SLA24H = s.slaText(ctx, out[i].NodeID, 24)
		out[i].SLA7D = s.slaText(ctx, out[i].NodeID, 24*7)
		out[i].SLA30D = s.slaText(ctx, out[i].NodeID, 24*30)
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_bytes), 0)
			FROM daily_node_traffic_stats WHERE node_id = ?`, out[i].NodeID).
			Scan(&out[i].TotalSentBytes)
	}
	return out, nil
}
