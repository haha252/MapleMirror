package syncer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mirror-server/internal/protocol"
	"mirror-server/internal/storage"
)

func TestDownloadUsesPrimarySourceWhenAvailable(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primaryHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	fallbackHits := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("primary download should succeed: %+v", result)
	}
	if primaryHits != 1 || fallbackHits != 0 {
		t.Fatalf("unexpected hits primary=%d fallback=%d", primaryHits, fallbackHits)
	}
}

func TestDownloadFallsBackToPeerWhenPrimaryFails(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer peer-token" {
			t.Fatalf("missing peer token header: %q", got)
		}
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" || result.LocalDigestSHA256 != digest("abcdef") {
		t.Fatalf("fallback download should succeed: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "p1", "v1", "a.zip")); err != nil {
		t.Fatalf("fallback asset should be stored: %v", err)
	}
}

func TestDownloadPreflightsPrimaryAndSkipsFileGetWhenSourceUnavailable(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	var headHits, getHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headHits++
			http.Error(w, "github unavailable", http.StatusBadGateway)
			return
		}
		getHits++
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer primary.Close()
	var fallbackHits int
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL+"/asset.zip", fallback.URL, digest("abcdef"), 6)
	executor := Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		Probe: NewSourceProbe(primary.Client()), AllowPrivateSourceURLs: true}
	result := executor.download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("fallback download should succeed after failed preflight: %+v", result)
	}
	if headHits != 1 || getHits != 0 || fallbackHits != 1 {
		t.Fatalf("unexpected hits head=%d get=%d fallback=%d", headHits, getHits, fallbackHits)
	}

	second := fallbackTask(primary.URL+"/asset2.zip", fallback.URL, digest("abcdef"), 6)
	second.TaskID = "task-2"
	result = executor.download(context.Background(), second)
	if result.Result != "succeeded" {
		t.Fatalf("cached failed preflight should still use fallback: %+v", result)
	}
	if headHits != 1 || getHits != 0 || fallbackHits != 1 {
		t.Fatalf("preflight should be cached head=%d get=%d fallback=%d", headHits, getHits, fallbackHits)
	}
}

func TestSourceProbeCoalescesConcurrentChecks(t *testing.T) {
	var headHits int
	release := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headHits++
		<-release
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	probe := NewUnsafeSourceProbe(primary.Client())
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = probe.Check(context.Background(), primary.URL+"/asset.zip")
		}()
	}
	close(release)
	wg.Wait()
	if headHits != 1 {
		t.Fatalf("concurrent source checks should share one probe, got %d", headHits)
	}
	_ = probe.Check(context.Background(), primary.URL+"/asset2.zip")
	if headHits != 1 {
		t.Fatalf("failed probe should be cached, got %d", headHits)
	}
}

func TestDownloadReturnsTemporaryErrorWhenSourceUnavailableAndNoPeer(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	var getHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "github unavailable", http.StatusBadGateway)
			return
		}
		getHits++
		_, _ = w.Write([]byte("should-not-fetch"))
	}))
	defer primary.Close()

	task := fallbackTask(primary.URL+"/asset.zip", "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{
		DB: db, Storage: storageDir, TempDir: tempDir, Probe: NewSourceProbe(primary.Client()),
		AllowPrivateSourceURLs: true,
	}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("unavailable source with no peer should be temporary_error: %+v", result)
	}
	if getHits != 0 {
		t.Fatalf("primary file should not be fetched after failed preflight, getHits=%d", getHits)
	}
}

func TestDownloadRejectsPeerDigestMismatch(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "digest_mismatch" {
		t.Fatalf("peer digest mismatch should fail: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "p1", "v1", "a.zip")); !os.IsNotExist(err) {
		t.Fatalf("mismatched peer asset should not be stored: %v", err)
	}
}

func prepareSyncer(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenNode(filepath.Join(dir, "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, filepath.Join(dir, "assets"), filepath.Join(dir, "tmp")
}

func fallbackTask(primaryURL, fallbackURL, wantDigest string, size int64) protocol.SyncTask {
	return protocol.SyncTask{
		TaskID: "task-1", TaskType: "asset_download",
		Asset: protocol.SyncAsset{
			AssetID: "asset-1", ProjectID: "p1", Version: "v1", FileName: "a.zip",
			SizeBytes: size, DownloadURL: primaryURL, DigestSHA256: wantDigest,
		},
		FallbackSources: []protocol.SyncFallbackSource{{
			NodeID: "node-2", NodeName: "源节点", DownloadURL: fallbackURL, Token: "peer-token",
		}},
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
