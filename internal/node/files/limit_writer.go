package files

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"mirror-server/internal/downloadtoken"
)

const (
	trafficCheckpointBytes    = int64(8 << 20)
	trafficCheckpointInterval = 30 * time.Second
)

type limitCountingWriter struct {
	http.ResponseWriter
	handler          *Handler
	authorizationID  string
	claims           downloadtoken.Claims
	limit            int64
	sent             int64
	bytes            int64
	timing           authorizationTiming
	assetID          string
	nodeRequestID    string
	checkpointBytes  int64
	lastCheckpointAt time.Time
	requestContext   context.Context
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
	w.setWriteDeadline(now)
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
		w.addCheckpointBytes(int64(n))
		w.timing.LastWriteAt = time.Now().UTC()
		_ = w.handler.touchAuthorization(w.authorizationID, w.timing.LastWriteAt)
		if !w.lastCheckpointAt.IsZero() && time.Since(w.lastCheckpointAt) >= trafficCheckpointInterval {
			w.flushTrafficCheckpoint(false)
		}
	}
	if err != nil {
		return n, err
	}
	if n < len(data) || grant < int64(len(data)) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (w *limitCountingWriter) addCheckpointBytes(bytes int64) {
	for bytes > 0 {
		remaining := trafficCheckpointBytes - w.checkpointBytes
		if remaining <= 0 {
			w.flushTrafficCheckpoint(false)
			remaining = trafficCheckpointBytes
		}
		if bytes < remaining {
			w.checkpointBytes += bytes
			return
		}
		w.checkpointBytes += remaining
		bytes -= remaining
		w.flushTrafficCheckpoint(false)
	}
}

func (w *limitCountingWriter) flushTrafficCheckpoint(force bool) {
	if w.checkpointBytes <= 0 || (!force && w.checkpointBytes < trafficCheckpointBytes &&
		(w.lastCheckpointAt.IsZero() || time.Since(w.lastCheckpointAt) < trafficCheckpointInterval)) {
		return
	}
	bytes := w.checkpointBytes
	w.checkpointBytes = 0
	w.lastCheckpointAt = time.Now().UTC()
	if err := w.handler.recordTraffic(w.claims, w.assetID, w.nodeRequestID, bytes); err != nil && w.handler.Logger != nil {
		ctx := w.requestContext
		if ctx == nil {
			ctx = context.Background()
		}
		w.handler.Logger.Warn(ctx, "下载流量事件记录失败",
			slog.String("request_id", w.nodeRequestID),
			slog.String("authorization_id", w.authorizationID),
			slog.String("asset_id", w.assetID),
			slog.Int64("bytes", bytes),
			slog.String("error", err.Error()))
	}
}

func (w *limitCountingWriter) setWriteDeadline(now time.Time) {
	deadline := w.timing.MaxDeadline
	if w.timing.IdleTimeout > 0 {
		idleDeadline := now.Add(w.timing.IdleTimeout)
		if deadline.IsZero() || idleDeadline.Before(deadline) {
			deadline = idleDeadline
		}
	}
	if deadline.IsZero() {
		return
	}
	_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
}

func (w *limitCountingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
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
