package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mirror-server/internal/controltls"
)

type createPairingRequest struct {
	TTLSeconds int `json:"ttl_seconds"`
}

func (s Server) pairingCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		id := strings.TrimPrefix(r.URL.Path, "/api/admin/v1/pairing-codes/")
		s.revokePairing(w, r, id)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/admin/v1/pairing-codes" {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
		return
	}
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	var body createPairingRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	ttl := 5 * time.Minute
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	item, err := s.Repo.CreatePairing(r.Context(), ttl, requestID(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "创建配对码失败")
		return
	}
	writeOK(w, r, http.StatusCreated, "一次性配对码已创建，请安全传递并在有效期内使用", map[string]any{
		"pairing_code_id": item.ID, "pairing_code": item.Code,
		"expires_at": item.ExpiresAt,
	})
}

func (s Server) pairingRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
		return
	}
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	items, err := s.Repo.PendingEnrollments(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "查询登记请求失败")
		return
	}
	writeOK(w, r, http.StatusOK, "登记请求列表", items)
}

func (s Server) pairingRequestByID(w http.ResponseWriter, r *http.Request) {
	id, action := splitPairingPath(r.URL.Path)
	if id == "" {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "登记请求不存在")
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		s.pairingDetail(w, r, id)
	case r.Method == http.MethodPost && action == "approve":
		s.approvePairing(w, r, id)
	case r.Method == http.MethodPost && action == "reject":
		s.rejectPairing(w, r, id)
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
	}
}

func (s Server) revokePairing(w http.ResponseWriter, r *http.Request, id string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	if err := s.Repo.RevokePairing(r.Context(), id, requestID(r)); err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "配对码不可撤销")
		return
	}
	_ = s.Repo.Audit(r.Context(), "pairing_code.revoke", "pairing_code", id, "success", requestID(r), "配对码已撤销", adminID)
	writeOK(w, r, http.StatusOK, "配对码已撤销", map[string]string{"pairing_code_id": id})
}

func (s Server) pairingDetail(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	csr, item, err := s.Repo.CSRForEnrollment(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "登记请求不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "登记请求详情", map[string]any{"enrollment": item, "csr_pem": csr})
}

type approveRequest struct {
	ConfirmedPublicName           string `json:"confirmed_public_name"`
	ConfirmedPublicKeyFingerprint string `json:"confirmed_public_key_fingerprint"`
}

func (s Server) approvePairing(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	var body approveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不合法")
		return
	}
	csrPEM, item, err := s.Repo.CSRForEnrollment(r.Context(), id)
	if err != nil || item.Status != "pending" {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "登记请求不存在")
		return
	}
	csr, err := controltls.ParseCSR([]byte(csrPEM))
	if err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "CSR 不合法")
		return
	}
	if s.Signer == nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "证书签发材料尚未配置")
		return
	}
	signed, err := s.Signer(csr)
	if err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", fmt.Sprintf("证书签发失败：%v", err))
		return
	}
	err = s.Repo.ApproveEnrollment(r.Context(), id, body.ConfirmedPublicName,
		body.ConfirmedPublicKeyFingerprint, requestID(r), signed)
	if err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", err.Error())
		return
	}
	writeOK(w, r, http.StatusOK, "登记请求已批准", map[string]string{"node_id": signed.NodeID})
}

func (s Server) rejectPairing(w http.ResponseWriter, r *http.Request, id string) {
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.Repo.RejectEnrollment(r.Context(), id, requestID(r), body.Reason, adminID); err != nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", err.Error())
		return
	}
	writeOK(w, r, http.StatusOK, "登记请求已拒绝", map[string]string{"enrollment_id": id})
}

func splitPairingPath(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/admin/v1/pairing-requests/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
