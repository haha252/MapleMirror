package syncer

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestDownloadUsesUniqueTempPathForDuplicateTaskExecution(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	var primaryHits int
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	executor := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}
	var wg sync.WaitGroup
	results := make(chan protocol.SyncTaskResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- executor.download(context.Background(), task)
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.Result != "succeeded" {
			t.Fatalf("duplicate task execution should not lose temp file: %+v", result)
		}
	}
	if got, _, err := fileDigest(filepath.Join(storageDir, relativeAssetPath(task.Asset))); err != nil ||
		got != digest("abcdef") {
		t.Fatalf("stored asset mismatch digest=%s err=%v", got, err)
	}
	if primaryHits != 1 {
		t.Fatalf("duplicate task execution should reuse verified asset, primary hits=%d", primaryHits)
	}
}

func TestConcurrentDuplicateFallbackDownloadReusesVerifiedAsset(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	var fallbackHits int
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()
	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	executor := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}
	var wg sync.WaitGroup
	results := make(chan protocol.SyncTaskResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- executor.download(context.Background(), task)
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.Result != "succeeded" {
			t.Fatalf("duplicate fallback download should succeed: %+v", result)
		}
	}
	if fallbackHits != 1 {
		t.Fatalf("duplicate fallback download should fetch peer once, hits=%d", fallbackHits)
	}
}

func TestPeerFallbackConcurrencyUsesDefaultLimit(t *testing.T) {
	maxActive := runPeerFallbackConcurrency(t, 0)
	if maxActive > defaultMaxConcurrentPeerFallbacks {
		t.Fatalf("peer fallback concurrency=%d, want <= %d", maxActive, defaultMaxConcurrentPeerFallbacks)
	}
}

func TestPeerFallbackConcurrencyCanBeRaised(t *testing.T) {
	maxActive := runPeerFallbackConcurrency(t, 5)
	if maxActive < 4 || maxActive > 5 {
		t.Fatalf("configured peer fallback concurrency=%d, want 4..5", maxActive)
	}
}

func runPeerFallbackConcurrency(t *testing.T, limit int) int {
	t.Helper()
	db, storageDir, tempDir := prepareSyncer(t)
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("abcdef"))
		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer fallback.Close()
	executor := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true, PeerFallbackMaxConcurrent: limit}
	var wg sync.WaitGroup
	results := make(chan protocol.SyncTaskResult, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
			task.TaskID = fmt.Sprintf("task-%d", i)
			task.Asset.AssetID = fmt.Sprintf("asset-%d", i)
			task.Asset.FileName = fmt.Sprintf("a-%d.zip", i)
			results <- executor.download(context.Background(), task)
		}(i)
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.Result != "succeeded" {
			t.Fatalf("fallback download should succeed: %+v", result)
		}
	}
	return maxActive
}
