package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchSwarmRangeRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		contentRange string
		body         string
		flush        bool
		wantOK       bool
	}{
		{name: "exact", status: http.StatusPartialContent, contentRange: "bytes 1-3/6", body: "bcd", wantOK: true},
		{name: "wrong content range", status: http.StatusPartialContent, contentRange: "bytes 0-2/6", body: "bcd"},
		{name: "wrong total", status: http.StatusPartialContent, contentRange: "bytes 1-3/7", body: "bcd"},
		{name: "missing content range", status: http.StatusPartialContent, body: "bcd"},
		{name: "origin ignores range", status: http.StatusOK, body: "abcdef"},
		{name: "short body", status: http.StatusPartialContent, contentRange: "bytes 1-3/6", body: "bc"},
		{name: "long chunked body", status: http.StatusPartialContent, contentRange: "bytes 1-3/6", body: "bcde", flush: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				w.WriteHeader(tc.status)
				if tc.flush {
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "partial")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			executor := Executor{Client: server.Client(), AllowPrivateSourceURLs: true}
			err = executor.fetchRangeInto(context.Background(), server.URL, "", 1, 4, 6, file)
			if (err == nil) != tc.wantOK {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
