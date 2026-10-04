package files

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type shortPayloadWriter struct{ http.ResponseWriter }

func (w shortPayloadWriter) Write(data []byte) (int, error) {
	n, _ := w.ResponseWriter.Write(data[:2])
	return n, errors.New("disconnected")
}

func TestPayloadCounterCountsOnlySuccessfulDownloadBodyBytes(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusPartialContent, http.StatusNotModified, http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable} {
		var bytes int64
		rec := httptest.NewRecorder()
		counter := &payloadCountingWriter{ResponseWriter: shortPayloadWriter{rec}, record: func(n int64) { bytes += n }}
		counter.WriteHeader(status)
		n, err := counter.Write([]byte("abcdef"))
		want := int64(0)
		wantN := 2
		if status == http.StatusNotModified {
			wantN = 0
		}
		if status == http.StatusOK || status == http.StatusPartialContent {
			want = 2
		}
		if n != wantN || err == nil || bytes != want {
			t.Fatalf("status=%d n=%d err=%v bytes=%d want=%d", status, n, err, bytes, want)
		}
	}
}
