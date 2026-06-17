package syncer

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"mirror-server/internal/protocol"
)

func TestDownloadFallsBackToPeerParts(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	data := []byte("abcdefghijklmnopqrstuvwxyz012345")
	var maxActive int64
	var active int64
	var arrivals int64
	var barrier sync.WaitGroup
	barrier.Add(2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt64(&active, 1)
		defer atomic.AddInt64(&active, -1)
		for {
			previous := atomic.LoadInt64(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt64(&maxActive, previous, current) {
				break
			}
		}
		if atomic.AddInt64(&arrivals, 1) <= 2 {
			barrier.Done()
			releaseOnce.Do(func() {
				go func() {
					barrier.Wait()
					close(release)
				}()
			})
			<-release
		}
		start, end := parseTestRange(t, r.Header.Get("Range"))
		if got := r.Header.Get("Authorization"); got == "" || !strings.HasPrefix(got, "Bearer part-") {
			t.Fatalf("missing part token header: %q", got)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer fallback.Close()
	task := fallbackTask("http://primary.test:8080/asset.zip", fallback.URL, digest(string(data)), int64(len(data)))
	task.FallbackSources[0].Parts = testParts(int64(len(data)), 8)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: fallback.Client(),
		AllowPrivateSourceURLs: true, PeerFallbackWorkers: 8, PeerFallbackMinSize: 1}).download(context.Background(), task)
	if result.Result != "succeeded" || result.LocalDigestSHA256 != digest(string(data)) {
		t.Fatalf("fallback parts should succeed: %+v", result)
	}
	stored, err := os.ReadFile(filepath.Join(storageDir, "p1", "v1", "a.zip"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored data mismatch data=%q err=%v", string(stored), err)
	}
	if maxActive <= 1 {
		t.Fatalf("expected concurrent part downloads, maxActive=%d", maxActive)
	}
}

func TestDownloadPeerPartFailureCleansTemp(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	data := []byte("abcdefghijklmnopqrstuvwxyz012345")
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end := parseTestRange(t, r.Header.Get("Range"))
		if start > 0 {
			http.Error(w, "part failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	defer fallback.Close()
	task := fallbackTask("http://primary.test:8080/asset.zip", fallback.URL, digest(string(data)), int64(len(data)))
	task.FallbackSources[0].Parts = testParts(int64(len(data)), 8)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: fallback.Client(),
		AllowPrivateSourceURLs: true, PeerFallbackWorkers: 8, PeerFallbackMinSize: 1}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("failed part should be temporary_error: %+v", result)
	}
	assertTempDirEmpty(t, tempDir)
}

func testParts(size int64, count int) []protocol.SyncFallbackPart {
	partSize := (size + int64(count) - 1) / int64(count)
	parts := make([]protocol.SyncFallbackPart, 0, count)
	for start := int64(0); start < size; start += partSize {
		end := start + partSize - 1
		if end >= size {
			end = size - 1
		}
		parts = append(parts, protocol.SyncFallbackPart{
			RangeStart: start,
			RangeEnd:   end,
			Token:      fmt.Sprintf("part-%d-%d", start, end),
		})
	}
	return parts
}

func parseTestRange(t *testing.T, value string) (int64, int64) {
	t.Helper()
	value = strings.TrimPrefix(value, "bytes=")
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		t.Fatalf("bad range %q", value)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return start, end
}
