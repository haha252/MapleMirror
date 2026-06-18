package syncer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
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
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	fallbackHits := 0
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
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
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer peer-token" {
			t.Fatalf("missing peer token header: %q", got)
		}
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" || result.LocalDigestSHA256 != digest("abcdef") {
		t.Fatalf("fallback download should succeed: %+v", result)
	}
	if !result.PeerFallbackAttempted {
		t.Fatalf("successful fallback should report peer attempt: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "p1", "v1", "a.zip")); err != nil {
		t.Fatalf("fallback asset should be stored: %v", err)
	}
}

func TestDownloadReportsPeerAttemptWhenFallbackCommitFails(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storageDir, "p1"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("commit failure should be temporary_error: %+v", result)
	}
	if !result.PeerFallbackAttempted {
		t.Fatalf("commit failure after fallback should report peer attempt: %+v", result)
	}
}

func TestDownloadFallsBackAfterPrimaryGetFailure(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	var getHits int
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getHits++
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	var fallbackHits int
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits++
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL+"/asset.zip", fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("fallback download should succeed after primary failure: %+v", result)
	}
	if getHits != 1 || fallbackHits != 1 {
		t.Fatalf("unexpected hits get=%d fallback=%d", getHits, fallbackHits)
	}
}

func TestSourceProbeCoalescesConcurrentChecks(t *testing.T) {
	var headHits int
	release := make(chan struct{})
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getHits++
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()

	task := fallbackTask(primary.URL+"/asset.zip", "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{
		DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true,
	}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("unavailable source with no peer should be temporary_error: %+v", result)
	}
	if getHits != 1 {
		t.Fatalf("primary file should be fetched once after direct failure, getHits=%d", getHits)
	}
}

func TestDownloadRejectsPeerDigestMismatch(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdeg"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "digest_mismatch" {
		t.Fatalf("peer digest mismatch should fail: %+v", result)
	}
	if !result.PeerFallbackAttempted {
		t.Fatalf("peer digest mismatch should report fallback attempt: %+v", result)
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
