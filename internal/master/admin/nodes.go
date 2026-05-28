package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mirror-server/internal/controltls"
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
	case r.Method == http.MethodPost && action == "enable":
		s.enableNode(w, r, nodeID)
	case r.Method == http.MethodPost && action == "sync-reset":
		s.syncReset(w, r, nodeID)
	case r.Method == http.MethodGet && action == "sync-status":
		s.syncStatus(w, r, nodeID)
	case r.Method == http.MethodPost && strings.HasPrefix(action, "sync-tasks/"):
		s.syncTaskAction(w, r, nodeID, action)
	case r.Method == http.MethodPost && action == "certificates/rotate":
		s.rotateCertificate(w, r, nodeID)
	case r.Method == http.MethodGet && strings.HasSuffix(action, "latest"):
		s.latestReport(w, r, nodeID, action)
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

func (s Server) enableNode(w http.ResponseWriter, r *http.Request, nodeID string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	if err := s.Repo.EnableNode(r.Context(), nodeID, requestID(r), adminID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "启用节点失败")
		return
	}
	writeOK(w, r, http.StatusOK, "节点已启用", map[string]any{"node_id": nodeID, "routing_ready": false})
}

func (s Server) rotateCertificate(w http.ResponseWriter, r *http.Request, nodeID string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	if s.Signer == nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "证书签发材料尚未配置")
		return
	}
	csrPEM, _, err := s.Repo.CSRForNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "节点 CSR 不存在")
		return
	}
	csr, err := controltls.ParseCSR([]byte(csrPEM))
	if err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "CSR 不合法")
		return
	}
	signed, err := s.Signer(csr)
	if err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", fmt.Sprintf("证书签发失败：%v", err))
		return
	}
	signed.NodeID = nodeID
	if err := s.Repo.RotateCertificate(r.Context(), nodeID, requestID(r), adminID, signed); err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "证书轮换失败")
		return
	}
	writeOK(w, r, http.StatusOK, "节点证书已轮换", map[string]any{"node_id": nodeID, "routing_ready": false})
}

func (s Server) latestReport(w http.ResponseWriter, r *http.Request, nodeID, action string) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	var data map[string]any
	var err error
	switch action {
	case "heartbeats/latest":
		data, err = s.Repo.LatestHeartbeat(r.Context(), nodeID)
	case "inventory-reports/latest":
		data, err = s.Repo.LatestInventoryReport(r.Context(), nodeID)
	case "pressure-reports/latest":
		data, err = s.Repo.LatestPressureReport(r.Context(), nodeID)
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "报告不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "最近报告", data)
}

func splitNodePath(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/admin/v1/nodes/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], "/")
}
