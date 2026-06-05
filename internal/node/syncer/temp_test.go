package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadKeepsTempFilesOutsideAssetTreeWhenConfiguredInside(t *testing.T) {
	db, storageDir, _ := prepareSyncer(t)
	tempDir := filepath.Join(storageDir, "tmp")
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir}).download(context.Background(), task)
	if result.Result != "digest_mismatch" {
		t.Fatalf("expected digest mismatch, got %+v", result)
	}
	entries, err := os.ReadDir(storageDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), task.TaskID) || entry.Name() == "tmp" {
			t.Fatalf("temporary file or dir should not appear in asset tree: %s", entry.Name())
		}
	}
}

func TestDownloadReplacesStaleIncompleteTargetAfterValidation(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	finalPath := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(finalPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir}).download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("validated download should replace stale target: %+v", result)
	}
	data, err := os.ReadFile(finalPath)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("target was not replaced correctly data=%q err=%v", string(data), err)
	}
}
