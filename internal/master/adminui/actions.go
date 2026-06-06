package adminui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mirror-server/internal/controltls"
)

func (s *Server) scanAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.syncStore.ListProjectScanStates(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "扫描状态查询失败"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": s.scanStatesResponse(items)})
	case http.MethodPost:
		if _, ok := s.requireHighRisk(w, r); !ok {
			return
		}
		if s.sync == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "扫描服务未启用"})
			return
		}
		var body struct {
			ProjectID string `json:"project_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		scanID, err := s.sync.Trigger(r.Context(), body.ProjectID, requestID(r))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "创建扫描任务失败"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"message": "Release 扫描任务已创建", "scan_id": scanID})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
	}
}

func (s *Server) projectActionAPI(w http.ResponseWriter, r *http.Request) {
	projectID, action := splitAdminPath(r.URL.Path, "/admin/api/projects/")
	if r.Method != http.MethodPost || action != "reset" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if s.sync == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "扫描服务未启用"})
		return
	}
	if !s.projectEnabled(r, projectID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "项目不存在"})
		return
	}
	if err := s.syncStore.ResetProject(r.Context(), projectID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "项目重置失败"})
		return
	}
	scanID, err := s.sync.Trigger(r.Context(), projectID, requestID(r))
	if err != nil {
		_ = s.repo.Audit(r.Context(), "project.reset", "project", projectID, "failed", requestID(r), "项目数据已清空，重新扫描失败", admin)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "项目重置后重新扫描失败"})
		return
	}
	_ = s.repo.Audit(r.Context(), "project.reset", "project", projectID, "success", requestID(r), "项目数据已重置并重新扫描", admin)
	writeJSON(w, http.StatusOK, map[string]any{"message": "项目数据已重置并重新扫描", "scan_id": scanID})
}

func (s *Server) projectEnabled(r *http.Request, projectID string) bool {
	if s.projects == nil {
		return true
	}
	projects, err := s.projects.Load()
	if err != nil {
		projects = s.projects.Current()
	}
	for _, project := range projects.Projects {
		if project.ID == projectID {
			return project.Enabled
		}
	}
	return false
}

func (s *Server) pairingCodesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	var body struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ttl := 5 * time.Minute
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	item, err := s.repo.CreatePairing(r.Context(), ttl, requestID(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "创建配对码失败"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "一次性配对码已创建", "pairing_code_id": item.ID,
		"pairing_code": item.Code, "expires_at": item.ExpiresAt.In(s.location()).Format(adminTimeLayout),
	})
}

func (s *Server) pairingRequestsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	items, err := s.repo.PendingEnrollments(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "登记请求查询失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.pairingRequestsResponse(items)})
}

func (s *Server) pairingRequestActionAPI(w http.ResponseWriter, r *http.Request) {
	id, action := splitAdminPath(r.URL.Path, "/admin/api/pairing-requests/")
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "登记请求不存在"})
		return
	}
	if r.Method == http.MethodGet {
		s.pairingDetail(w, r, id)
		return
	}
	if r.Method == http.MethodPost && action == "approve" {
		s.approvePairing(w, r, id)
		return
	}
	if r.Method == http.MethodPost && action == "reject" {
		s.rejectPairing(w, r, id)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
}

func (s *Server) pairingDetail(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	csr, item, err := s.repo.CSRForEnrollment(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "登记请求不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enrollment": item, "csr_pem": csr})
}

func (s *Server) approvePairing(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	var body struct {
		ConfirmedPublicName           string `json:"confirmed_public_name"`
		ConfirmedPublicKeyFingerprint string `json:"confirmed_public_key_fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "请求内容不合法"})
		return
	}
	csrPEM, item, err := s.repo.CSRForEnrollment(r.Context(), id)
	if err != nil || item.Status != "pending" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "登记请求不存在"})
		return
	}
	csr, err := controltls.ParseCSR([]byte(csrPEM))
	if err != nil || s.signer == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "CSR 或签发材料不可用"})
		return
	}
	signed, err := s.signer(csr)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": fmt.Sprintf("证书签发失败：%v", err)})
		return
	}
	err = s.repo.ApproveEnrollment(r.Context(), id, body.ConfirmedPublicName,
		body.ConfirmedPublicKeyFingerprint, requestID(r), signed)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "登记请求已批准", "node_id": signed.NodeID})
}

func (s *Server) rejectPairing(w http.ResponseWriter, r *http.Request, id string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.repo.RejectEnrollment(r.Context(), id, requestID(r), body.Reason, admin); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "登记请求已拒绝", "enrollment_id": id})
}
