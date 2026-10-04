package files

import "net/http"

type payloadCountingWriter struct {
	http.ResponseWriter
	record func(int64)
	status int
}

func (w *payloadCountingWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *payloadCountingWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	if n > 0 && (w.status == http.StatusOK || w.status == http.StatusPartialContent) {
		w.record(int64(n))
	}
	return n, err
}

func (w *payloadCountingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (h *Handler) payloadResponseWriter(w http.ResponseWriter, swarm bool) http.ResponseWriter {
	if h.Activity == nil {
		return w
	}
	record := h.Activity.RecordPublicBytes
	if swarm {
		record = h.Activity.RecordSwarmBytes
	}
	return &payloadCountingWriter{ResponseWriter: w, record: record}
}
