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
	countSQL := `SELECT COUNT(*) FROM traffic_event_dedupe`
	countArgs := []any{}
	whereSQL := ""
	if authID != "" {
		whereSQL = ` WHERE authorization_id = ?`
		countArgs = append(countArgs, authID)
	}
	if err := s.repo.DB.QueryRowContext(r.Context(), countSQL+whereSQL, countArgs...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件查询失败"})
		return
	}
	queryArgs := append([]any{}, countArgs...)
	queryArgs = append(queryArgs, page.PageSize, page.offset())
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT node_id, event_sequence,
		authorization_id, event_hash, COALESCE(accounted_at, '') FROM traffic_event_dedupe
		`+whereSQL+` ORDER BY COALESCE(accounted_at, '') DESC, node_id, event_sequence LIMIT ? OFFSET ?`,
		queryArgs...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var nodeID, authID, hash, accounted string
		var seq int64
		if err := rows.Scan(&nodeID, &seq, &authID, &hash, &accounted); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件读取失败"})
			return
		}
		items = append(items, map[string]any{"node_id": nodeID,
			"event_sequence": seq, "authorization_id": authID,
			"event_hash":   hash,
			"accounted_at": s.displayTime(accounted)})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "流量事件读取失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "pagination": page})
}
