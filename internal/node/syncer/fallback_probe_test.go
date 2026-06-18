package syncer

import (
	"context"
	"net/http"
	"testing"
)

func TestDownloadUsesSourceProbeBeforePrimaryWhenFallbackExists(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	var headHits, getHits int
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headHits++
			http.Error(w, "unavailable", http.StatusBadGateway)
		case http.MethodGet:
			getHits++
			_, _ = w.Write([]byte("should-not-fetch"))
		}
	}))
	defer primary.Close()
	var fallbackHits int
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL+"/asset.zip", fallback.URL, digest("abcdef"), 6)
	executor := Executor{
		DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		Probe: NewSourceProbe(primary.Client()), AllowPrivateSourceURLs: true,
	}
	result := executor.download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("probe-backed fallback should succeed: %+v", result)
	}
	if headHits != 1 || getHits != 0 || fallbackHits != 1 {
		t.Fatalf("unexpected hits head=%d get=%d fallback=%d", headHits, getHits, fallbackHits)
	}
}
