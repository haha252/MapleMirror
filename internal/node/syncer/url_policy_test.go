package syncer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchWithTokenRejectsLoopbackBeforeRequest(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer server.Close()

	_, _, err := (Executor{}).fetchWithToken(context.Background(),
		server.URL, filepath.Join(t.TempDir(), "asset.tmp"), "")
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected private source rejection, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("unsafe source should not receive request, hits=%d", hits)
	}
}

func TestFetchWithTokenRejectsRedirectToPrivateHost(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {"http://127.0.0.1/secret"}},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}
	_, _, err := (Executor{Client: client}).fetchWithToken(context.Background(),
		"https://example.com/asset.zip", filepath.Join(t.TempDir(), "asset.tmp"), "")
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected redirect target rejection, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("redirect target should be rejected before second request, requests=%d", requests)
	}
}

func TestSourceProbeRejectsLoopbackBeforeRequest(t *testing.T) {
	hits := 0
	probe := NewSourceProbe(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits++
		return nil, errors.New("unexpected request")
	})})
	err := probe.Check(context.Background(), "http://127.0.0.1/asset.zip")
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected private source rejection, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("unsafe probe should not send HEAD, hits=%d", hits)
	}
}

func TestFetchFallbackRejectsUnsafePeerURL(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer server.Close()
	task := fallbackTask("https://example.com/asset.zip", server.URL, digest("abcdef"), 6)
	tmp := filepath.Join(t.TempDir(), "asset.tmp")
	_, _, err := (Executor{}).fetchFallback(context.Background(), task, tmp)
	if err == nil || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected unsafe fallback rejection, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("unsafe fallback should not receive request, hits=%d", hits)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("unsafe fallback should not leave temp file, err=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
