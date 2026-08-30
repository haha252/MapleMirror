package developerapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mirror-server/internal/requestid"
)

func developerSyncPath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 6 ||
		parts[0] != "api" || parts[1] != "developer" || parts[2] != "v1" ||
		parts[3] != "projects" || parts[4] == "" || parts[5] != "sync" {
		return "", false
	}
	return parts[4], true
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") ||
		strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func developerRequestID(r *http.Request) string {
	if id := requestid.FromContext(r.Context()); id != "" {
		return id
	}
	return r.Header.Get("X-Request-ID")
}

func (h *Handler) rateHeaders(w http.ResponseWriter, usage Usage) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(usage.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(usage.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(usage.ResetAt.Unix(), 10))
}

func retryAfter(now, target time.Time) string {
	seconds := int64(target.Sub(now).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

func (h *Handler) writeOK(w http.ResponseWriter, r *http.Request,
	code int, message string, data any) {
	h.writeJSON(w, code, apiResponse{
		Status: "success", Message: message,
		RequestID: developerRequestID(r), Data: data,
	})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request,
	code int, stable, message string) {
	h.writeJSON(w, code, apiResponse{
		Status: "error", Code: stable, Message: message,
		RequestID: developerRequestID(r),
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, code int, body apiResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
