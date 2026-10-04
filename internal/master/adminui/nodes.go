package adminui

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mirror-server/internal/geoip"
	mastercontrol "mirror-server/internal/master/control"
)

func (s *Server) nodeActionAPI(w http.ResponseWriter, r *http.Request) {
	nodeID, action := splitAdminPath(r.URL.Path, "/admin/api/nodes/")
	if nodeID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "sync-status":
		s.nodeSyncStatus(w, r, nodeID)
	case r.Method == http.MethodGet && action == "reports":
		s.nodeReports(w, r, nodeID)
	case r.Method == http.MethodGet && action == "sla":
		s.nodeSLA(w, r, nodeID)
	case action == "projects":
		s.nodeProjects(w, r, nodeID)
	case r.Method == http.MethodPost && action == "disable":
		s.disableNode(w, r, nodeID)
	case r.Method == http.MethodPost && action == "enable":
		s.enableNode(w, r, nodeID)
	case r.Method == http.MethodPost && action == "priority":
		s.updateNodePriority(w, r, nodeID)
	case r.Method == http.MethodPost && action == "region":
		s.updateNodeRegion(w, r, nodeID)
	case r.Method == http.MethodPost && action == "sync-reset":
		s.syncReset(w, r, nodeID)
	case r.Method == http.MethodDelete && action == "":
		s.deleteNode(w, r, nodeID)
	case r.Method == http.MethodPost && strings.HasPrefix(action, "sync-tasks/"):
		s.syncTask(w, r, nodeID, action)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
	}
}

func (s *Server) nodeSyncStatus(w http.ResponseWriter, r *http.Request, nodeID string) {
	item, err := s.syncStore.SyncStatus(r.Context(), nodeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "同步状态不存在"})
		return
	}
	writeJSON(w, http.StatusOK, s.syncStatusResponse(item))
}

func (s *Server) nodeReports(w http.ResponseWriter, r *http.Request, nodeID string) {
	data := map[string]any{}
	if item, err := s.repo.LatestHeartbeat(r.Context(), nodeID); err == nil {
		data["heartbeat"] = s.displayTimeMap(item)
	}
	if item, err := s.repo.LatestInventoryReport(r.Context(), nodeID); err == nil {
		data["inventory"] = s.displayTimeMap(item)
	}
	if item, err := s.repo.LatestPressureReport(r.Context(), nodeID); err == nil {
		data["pressure"] = s.displayTimeMap(item)
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) nodeSLA(w http.ResponseWriter, r *http.Request, nodeID string) {
	items := []map[string]any{
		slaWindow(s.repo.DB, r, nodeID, "24h", 24),
		slaWindow(s.repo.DB, r, nodeID, "7d", 24*7),
		slaWindow(s.repo.DB, r, nodeID, "30d", 24*30),
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_id": nodeID, "windows": items})
}

func (s *Server) disableNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	var body struct{ Reason string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.repo.DisableNode(r.Context(), nodeID, requestID(r), body.Reason); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "禁用节点失败"})
		return
	}
	_ = s.repo.Audit(r.Context(), "node.disable.mtls", "node", nodeID, "success", requestID(r), "管理者mTLS 已校验", admin)
	writeJSON(w, http.StatusOK, map[string]any{"message": "节点已禁用", "node_id": nodeID})
}

func (s *Server) enableNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if err := s.repo.EnableNode(r.Context(), nodeID, requestID(r), admin); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "启用节点失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "节点已启用", "node_id": nodeID})
}

func (s *Server) updateNodePriority(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	var body struct {
		DownloadPriority *int `json:"download_priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "请求体无效"})
		return
	}
	if body.DownloadPriority == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "下载优先级必须在 0-100 之间"})
		return
	}
	priority := *body.DownloadPriority
	if err := s.repo.UpdateNodeDownloadPriority(r.Context(), nodeID, priority); err != nil {
		switch {
		case errors.Is(err, mastercontrol.ErrInvalidDownloadPriority):
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "下载优先级必须在 0-100 之间"})
		case err == sql.ErrNoRows:
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "更新节点下载优先级失败"})
		}
		return
	}
	_ = s.repo.Audit(r.Context(), "node.download_priority", "node", nodeID, "success",
		requestID(r), "节点下载优先级已更新", admin)
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "节点下载优先级已更新", "node_id": nodeID,
		"download_priority": priority,
	})
}

func (s *Server) updateNodeRegion(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	var body struct {
		Region geoip.Region `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "请求体无效"})
		return
	}
	if err := s.repo.UpdateNodeRegion(r.Context(), nodeID, body.Region); err != nil {
		switch {
		case errors.Is(err, mastercontrol.ErrInvalidNodeRegion):
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "节点地区无效"})
		case err == sql.ErrNoRows:
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "更新节点地区失败"})
		}
		return
	}
	_ = s.repo.Audit(r.Context(), "node.region", "node", nodeID, "success",
		requestID(r), "节点地区已更新", admin)
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "节点地区已更新", "node_id": nodeID, "region": body.Region,
	})
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if err := s.repo.DeleteNode(r.Context(), nodeID, requestID(r), admin); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "删除节点失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "节点已删除", "node_id": nodeID})
}

func (s *Server) syncReset(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if err := s.repo.SyncReset(r.Context(), nodeID, requestID(r), admin); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步状态重置失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "已重新开始同步，保留已验证文件与分片", "node_id": nodeID})
}

func (s *Server) syncTask(w http.ResponseWriter, r *http.Request, nodeID, action string) {
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	taskID, op := splitTaskAction(action)
	var err error
	if op == "retry" {
		err = s.syncStore.RetryTask(r.Context(), nodeID, taskID)
	} else if op == "cancel" {
		err = s.syncStore.CancelTask(r.Context(), nodeID, taskID)
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "同步任务不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "同步任务操作失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "同步任务已更新", "task_id": taskID})
}
