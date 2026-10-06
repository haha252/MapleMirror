package adminui

import (
	"net/http"
	"strings"
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
	out := s.scanSummaryResponse(item)
	out["project_name"] = s.projectNames(r.Context())[item.ProjectID]
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) syncTasksAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	page := paginationFrom(r, 20)
	where, args := " WHERE 1 = 1", []any{}
	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		where += " AND t.node_id = ?"
		args = append(args, nodeID)
	}
	order := " ORDER BY t.created_at DESC, t.id DESC"
	if r.URL.Query().Get("active") == "1" {
		where += " AND t.state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')"
		order = " ORDER BY CASE t.state WHEN 'failed' THEN 0 WHEN 'retry_wait' THEN 1 ELSE 2 END, t.updated_at DESC, t.created_at DESC, t.id DESC"
	} else if state := r.URL.Query().Get("state"); state != "" {
		switch state {
		case "pending", "sent", "running", "retry_wait", "failed", "succeeded", "cancelled":
			where += " AND t.state = ?"
			args = append(args, state)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "未知任务状态"})
			return
		}
	}
	joins := " FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id " +
		"LEFT JOIN releases r ON r.id = a.release_id LEFT JOIN projects p ON p.id = r.project_id " +
		"LEFT JOIN nodes n ON n.id = t.node_id "
	countFrom := " FROM node_tasks t"
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		countFrom = joins
		where += " AND (instr(lower(COALESCE(a.file_name, '')), lower(?)) > 0 OR " +
			"instr(lower(COALESCE(p.name, r.project_id, '')), lower(?)) > 0 OR " +
			"instr(lower(COALESCE(n.public_name, t.node_id)), lower(?)) > 0 OR t.id = ?)"
		args = append(args, q, q, q, q)
	}
	var total int
	if err := s.repo.DB.QueryRowContext(r.Context(),
		"SELECT COUNT(*)"+countFrom+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务查询失败"})
		return
	}
	query := "SELECT t.id, t.node_id, t.task_type, COALESCE(t.asset_id, ''), t.state, " +
		"t.request_id, t.created_at, COALESCE(t.completed_at, ''), COALESCE(t.error_message, ''), " +
		"t.attempts, COALESCE(t.updated_at, ''), COALESCE(t.retry_after, ''), " +
		"COALESCE(t.lease_expires_at, ''), COALESCE(a.file_name, ''), " +
		"COALESCE(a.architecture, ''), COALESCE(a.system, ''), COALESCE(a.size_bytes, 0), " +
		"COALESCE(r.project_id, ''), COALESCE(r.tag_name, ''), COALESCE(p.name, ''), " +
		"COALESCE(n.public_name, '')" + joins + where + order + " LIMIT ? OFFSET ?"
	queryArgs := append(append([]any{}, args...), page.PageSize, page.offset())
	rows, err := s.repo.DB.QueryContext(r.Context(), query, queryArgs...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, node, typ, asset, state, req, created, completed, errText string
		var updated, retry, lease, fileName, arch, system, projectID, version, projectName, nodeName string
		var attempts int
		var size int64
		if err := rows.Scan(&id, &node, &typ, &asset, &state, &req, &created,
			&completed, &errText, &attempts, &updated, &retry, &lease, &fileName,
			&arch, &system, &size, &projectID, &version, &projectName, &nodeName); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务读取失败"})
			return
		}
		items = append(items, map[string]any{
			"task_id": id, "node_id": node, "node_name": nodeName, "task_type": typ,
			"asset_id": asset, "file_name": fileName, "project_id": projectID,
			"project_name": projectName, "version": version, "architecture": arch,
			"system": system, "size_bytes": size, "state": state, "request_id": req,
			"created_at": s.displayTime(created), "completed_at": s.displayTime(completed),
			"error_message": errText, "attempts": attempts, "updated_at": s.displayTime(updated),
			"retry_after": s.displayTime(retry), "lease_expires_at": s.displayTime(lease),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务读取失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items, "pagination": page})
}
