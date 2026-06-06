package files

import (
	"io"
	"net/http"
)

type limitCountingWriter struct {
	http.ResponseWriter
	handler         *Handler
	authorizationID string
	limit           int64
	sent            int64
	bytes           int64
}

func (w *limitCountingWriter) Write(data []byte) (int, error) {
	grant := w.handler.claimAuthorizationBytes(w.authorizationID, w.limit, w.sent, int64(len(data)))
	if grant <= 0 {
		return 0, io.ErrShortWrite
	}
	if grant < int64(len(data)) {
		data = data[:grant]
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
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
