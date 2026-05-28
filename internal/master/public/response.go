package public

import (
	"encoding/json"
	"net/http"

	"mirror-server/internal/requestid"
)

type response struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
	Data      any    `json:"data,omitempty"`
}

func requestID(r *http.Request) string {
	if id := requestid.FromContext(r.Context()); id != "" {
		return id
	}
	return r.Header.Get("X-Request-ID")
}

func writeOK(w http.ResponseWriter, r *http.Request, code int, message string, data any) {
	writeJSON(w, code, response{
		Status: "success", Message: message, RequestID: requestID(r), Data: data,
	})
}

func writeError(w http.ResponseWriter, r *http.Request, code int, stable, message string) {
	writeJSON(w, code, response{
		Status: "error", Code: stable, Message: message, RequestID: requestID(r),
	})
}

func writeJSON(w http.ResponseWriter, code int, body response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
