package syncer

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestFetchWithTokenAppliesBandwidthLimit(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdefghij"))
	}))
	defer server.Close()
	tmp := t.TempDir() + "/asset.tmp"
	started := time.Now()
	_, size, err := (Executor{BandwidthLimitBPS: 20, Client: server.Client(),
		AllowPrivateSourceURLs: true}).fetchWithToken(context.Background(), server.URL, tmp, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if size != 10 {
		t.Fatalf("download size = %d", size)
	}
	if elapsed := time.Since(started); elapsed < 350*time.Millisecond {
		t.Fatalf("bandwidth limit was not applied, elapsed=%s", elapsed)
	}
}
