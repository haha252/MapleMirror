package admin

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s Server) nodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
		return
	}
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	items, err := s.Repo.ListNodes(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "查询节点失败")
		return
	}
	writeOK(w, r, http.StatusOK, "节点列表", items)
}

func (s Server) nodeByID(w http.ResponseWriter, r *http.Request) {
	nodeID, action := splitNodePath(r.URL.Path)
	if nodeID == "" {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "节点不存在")
		return
	}
	switch {
	case r.Method == http.MethodPost && action == "disable":
		s.disableNode(w, r, nodeID)
	case r.Method == http.MethodPost && action == "sync-reset":
		s.syncReset(w, r, nodeID)
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
	}
}

func (s Server) disableNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.Repo.DisableNode(r.Context(), nodeID, requestID(r), body.Reason); err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "禁用节点失败")
		return
	}
	_ = s.Repo.Audit(r.Context(), "node.disable.mtls", "node", nodeID, "success", requestID(r), "管理员 mTLS 已验证", adminID)
	writeOK(w, r, http.StatusOK, "节点已禁用", map[string]any{"node_id": nodeID, "routing_ready": false})
}

func (s Server) syncReset(w http.ResponseWriter, r *http.Request, nodeID string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	if err := s.Repo.SyncReset(r.Context(), nodeID, requestID(r), adminID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "同步状态重置失败")
		return
	}
	writeOK(w, r, http.StatusOK, "同步状态已重置", map[string]any{"node_id": nodeID, "routing_ready": false})
}

func splitNodePath(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/admin/v1/nodes/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
