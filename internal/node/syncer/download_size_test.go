package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadStopsOversizedPrimaryWithoutFallback(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdefg"))
	}))
	defer primary.Close()

	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "size_mismatch" || result.SizeBytes != 7 {
		t.Fatalf("oversized primary should be rejected early: %+v", result)
	}
	assertTempDirEmpty(t, tempDir)
	if _, err := os.Stat(filepath.Join(storageDir, "p1", "v1", "a.zip")); !os.IsNotExist(err) {
		t.Fatalf("oversized primary asset should not be stored: %v", err)
	}
}

func TestDownloadRejectsOversizedPeerFallback(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdefg"))
	}))
	defer fallback.Close()

	task := fallbackTask(primary.URL, fallback.URL, digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir,
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "size_mismatch" || result.SizeBytes != 7 {
		t.Fatalf("oversized fallback should be rejected early: %+v", result)
	}
	assertTempDirEmpty(t, tempDir)
	if _, err := os.Stat(filepath.Join(storageDir, "p1", "v1", "a.zip")); !os.IsNotExist(err) {
		t.Fatalf("oversized fallback asset should not be stored: %v", err)
	}
}

func TestDownloadTimesOutWhenSourceStalls(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	stalled := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stalled
	}))
	defer func() {
		close(stalled)
		primary.Close()
	}()

	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{
		DB: db, Storage: storageDir, TempDir: tempDir,
		Client:                 &http.Client{Timeout: 30 * time.Millisecond},
		AllowPrivateSourceURLs: true,
	}).download(context.Background(), task)
	if result.Result != "temporary_error" {
		t.Fatalf("stalled source should time out: %+v", result)
	}
	assertTempDirEmpty(t, tempDir)
}

func assertTempDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir should be empty, got %d entries", len(entries))
	}
}
