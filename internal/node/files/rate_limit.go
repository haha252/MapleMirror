package files

import (
	"context"
	"io"
	"net/http"
)

type trafficLimiter interface {
	WrapWriter(context.Context, io.Writer) io.Writer
}

func (h *Handler) rateLimitedResponseWriter(r *http.Request,
	w http.ResponseWriter) http.ResponseWriter {
	if h.TrafficLimiter == nil {
		return w
	}
	return rateLimitedResponseWriter{ResponseWriter: w,
		writer: h.TrafficLimiter.WrapWriter(r.Context(), w)}
}

type rateLimitedResponseWriter struct {
	http.ResponseWriter
	writer interface {
		Write([]byte) (int, error)
	}
}

func (w rateLimitedResponseWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}
