package adminui

import "net/http"

func (s *Server) trafficEventsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	authID := r.URL.Query().Get("authorization_id")
	page := paginationFrom(r, 20)
	var total int
	if err := s.repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*)
		FROM traffic_events WHERE authorization_id = ?`, authID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件查询失败"})
		return
	}
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT node_id, event_sequence,
		authorization_id, node_request_id, master_request_id, sent_bytes, status,
		COALESCE(accounted_at, '') FROM traffic_events
		WHERE authorization_id = ? ORDER BY node_id, event_sequence LIMIT ? OFFSET ?`,
		authID, page.PageSize, page.offset())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var nodeID, authID, nodeReq, masterReq, state, accounted string
		var seq, bytes int64
		if err := rows.Scan(&nodeID, &seq, &authID, &nodeReq, &masterReq,
			&bytes, &state, &accounted); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件读取失败"})
			return
		}
		items = append(items, map[string]any{"node_id": nodeID,
			"event_sequence": seq, "authorization_id": authID,
			"node_request_id": nodeReq, "master_request_id": masterReq,
			"sent_bytes": bytes, "status": state,
			"accounted_at": s.displayTime(accounted)})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件读取失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "pagination": page})
}
