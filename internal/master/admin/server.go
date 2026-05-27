package admin

import (
	"crypto/x509"
	"encoding/json"
	"net/http"

	"mirror-server/internal/master/control"
	"mirror-server/internal/requestid"
)

type Server struct {
	Auth   Auth
	Repo   control.Repository
	Signer func(*x509.CertificateRequest) (control.SignedCertificate, error)
}

type response struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/v1/pairing-codes", s.pairingCodes)
	mux.HandleFunc("/api/admin/v1/pairing-requests", s.pairingRequests)
	mux.HandleFunc("/api/admin/v1/pairing-requests/", s.pairingRequestByID)
	return mux
}

func (s Server) require(w http.ResponseWriter, r *http.Request, highRisk bool) (string, bool) {
	identity, ok := s.Auth.Check(r, highRisk)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "ADMIN_AUTH_FAILED", "管理身份验证失败")
		return "", false
	}
	return identity, true
}

func requestID(r *http.Request) string {
	if id := requestid.FromContext(r.Context()); id != "" {
		return id
	}
	return r.Header.Get("X-Request-ID")
}

func writeOK(w http.ResponseWriter, r *http.Request, code int, message string, data any) {
	writeJSON(w, code, response{Status: "success", Message: message, RequestID: requestID(r), Data: data})
}

func writeError(w http.ResponseWriter, r *http.Request, code int, stable, message string) {
	writeJSON(w, code, response{Status: "error", Code: stable, Message: message, RequestID: requestID(r)})
}

func writeJSON(w http.ResponseWriter, code int, body response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
