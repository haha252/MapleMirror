package adminui

import (
	"net/http"
)

func (s *Server) latestScanAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	item, err := s.syncStore.LatestScan(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "扫描记录不存在"})
		return
	}
	writeJSON(w, http.StatusOK, s.scanSummaryResponse(item))
}

func (s *Server) syncTasksAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "node_id 不能为空"})
		return
	}
	page := paginationFrom(r, 20)
	var total int
	if err := s.repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*)
		FROM node_tasks WHERE node_id = ?`, nodeID).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务查询失败"})
		return
	}
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT id, node_id, task_type,
		COALESCE(asset_id, ''), state, request_id, created_at, COALESCE(completed_at, ''),
		COALESCE(error_message, ''), attempts, COALESCE(updated_at, ''),
		COALESCE(retry_after, ''), COALESCE(lease_expires_at, '')
		FROM node_tasks WHERE node_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		nodeID, page.PageSize, page.offset())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, node, typ, asset, state, req, created, completed string
		var errText, updated, retry, lease string
		var attempts int
		if err := rows.Scan(&id, &node, &typ, &asset, &state, &req, &created,
			&completed, &errText, &attempts, &updated, &retry, &lease); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务读取失败"})
			return
		}
		items = append(items, map[string]any{"task_id": id, "node_id": node,
			"task_type": typ, "asset_id": asset, "state": state,
			"request_id": req, "created_at": s.displayTime(created),
			"completed_at":  s.displayTime(completed),
			"error_message": errText, "attempts": attempts,
			"updated_at": s.displayTime(updated), "retry_after": s.displayTime(retry),
			"lease_expires_at": s.displayTime(lease)})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务读取失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items, "pagination": page})
}
