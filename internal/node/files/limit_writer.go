package files

import (
	"io"
	"net/http"
	"time"

	"mirror-server/internal/downloadtoken"
)

type limitCountingWriter struct {
	http.ResponseWriter
	handler         *Handler
	authorizationID string
	claims          downloadtoken.Claims
	limit           int64
	sent            int64
	bytes           int64
	timing          authorizationTiming
}

func (w *limitCountingWriter) Write(data []byte) (int, error) {
	now := time.Now().UTC()
	if !w.timing.MaxDeadline.IsZero() && now.After(w.timing.MaxDeadline) {
		_ = w.handler.expireAuthorization(w.claims, authorizationStatusExpiredMaxDuration, now)
		return 0, io.ErrShortWrite
	}
	if w.timing.IdleTimeout > 0 && !w.timing.LastWriteAt.IsZero() &&
		now.Sub(w.timing.LastWriteAt) > w.timing.IdleTimeout {
		_ = w.handler.expireAuthorization(w.claims, authorizationStatusExpiredIdle, now)
		return 0, io.ErrShortWrite
	}
	grant := w.handler.claimAuthorizationBytes(w.authorizationID, w.limit, w.sent, int64(len(data)))
	if grant <= 0 {
		return 0, io.ErrShortWrite
	}
	if grant < int64(len(data)) {
		data = data[:grant]
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	if n > 0 {
		w.timing.LastWriteAt = time.Now().UTC()
		_ = w.handler.touchAuthorization(w.authorizationID, w.timing.LastWriteAt)
	}
	if err != nil {
		return n, err
	}
	if n < len(data) || grant < int64(len(data)) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (h *Handler) claimAuthorizationBytes(id string, limit, sent, want int64) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.budgets == nil {
		h.budgets = make(map[string]int64)
	}
	remaining, ok := h.budgets[id]
	if !ok {
		remaining = limit - sent
	}
	if remaining <= 0 {
		h.budgets[id] = 0
		return 0
	}
	grant := want
	if grant > remaining {
		grant = remaining
	}
	h.budgets[id] = remaining - grant
	return grant
}
