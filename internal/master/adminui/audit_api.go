package adminui

import "net/http"

func (s *Server) auditEventsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	page := paginationFrom(r, 20)
	var total int
	if err := s.repo.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM admin_audit_events").Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "审计摘要查询失败"})
		return
	}
	query := "SELECT a.operation, a.target_type, a.target_id, a.result, a.request_id, " +
		"COALESCE(a.details_summary, ''), a.created_at, " +
		"COALESCE(NULLIF(n.public_name, ''), NULLIF(p.name, ''), a.target_id) " +
		"FROM admin_audit_events a " +
		"LEFT JOIN nodes n ON a.target_type = 'node' AND n.id = a.target_id " +
		"LEFT JOIN projects p ON a.target_type = 'project' AND p.id = a.target_id " +
		"ORDER BY a.created_at DESC LIMIT ? OFFSET ?"
	rows, err := s.repo.DB.QueryContext(r.Context(), query, page.PageSize, page.offset())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "审计摘要查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var op, targetType, targetID, result, reqID, summary, created, displayTarget string
		if err := rows.Scan(&op, &targetType, &targetID, &result, &reqID,
			&summary, &created, &displayTarget); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "审计摘要读取失败"})
			return
		}
		items = append(items, map[string]any{
			"operation": op, "target_type": targetType, "target_id": targetID,
			"target_name": displayTarget, "result": result, "request_id": reqID,
			"details_summary": summary, "created_at": s.displayTime(created),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "审计摘要读取失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "pagination": page})
}
