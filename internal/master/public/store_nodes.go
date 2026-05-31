package public

import "context"

func (s Store) Nodes(ctx context.Context) ([]NodeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_name, state,
		routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes ORDER BY public_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NodeSummary
	for rows.Next() {
		var item NodeSummary
		var ready int
		if err := rows.Scan(&item.NodeID, &item.PublicName, &item.State, &ready, &item.LastHeartbeat); err != nil {
			return nil, err
		}
		item.RoutingReady = ready == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
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
