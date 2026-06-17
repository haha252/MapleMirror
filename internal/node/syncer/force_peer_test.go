package syncer

import (
	"context"
	"net/http"
	"testing"
)

func TestDownloadForcePeerSkipsPrimarySource(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primaryHits := 0
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer primary.Close()
	fallbackHits := 0
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		if got := r.Header.Get("Authorization"); got != "Bearer peer-token" {
			t.Fatalf("missing peer token header: %q", got)
		}
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		ForcePeerDownload: true, AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" || result.LocalDigestSHA256 != digest("abcdef") {
		t.Fatalf("forced peer download should succeed: %+v", result)
	}
	if primaryHits != 0 || fallbackHits != 1 {
		t.Fatalf("unexpected hits primary=%d fallback=%d", primaryHits, fallbackHits)
	}
}

func TestDownloadForcePeerWithoutPeerReturnsTemporaryError(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primaryHits := 0
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer primary.Close()

	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		ForcePeerDownload: true, AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("forced peer download without peer should be temporary_error: %+v", result)
	}
	if primaryHits != 0 {
		t.Fatalf("primary source should be skipped, hits=%d", primaryHits)
	}
}
