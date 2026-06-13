package syncer

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadKeepsTempFilesOutsideAssetTreeWhenConfiguredInside(t *testing.T) {
	db, storageDir, _ := prepareSyncer(t)
	tempDir := filepath.Join(storageDir, "tmp")
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdeg"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
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

func TestCleanTempDirectoryRemovesOnlyChildren(t *testing.T) {
	dir := t.TempDir()
	tempDir := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(filepath.Join(tempDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "nested", "stale.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "stale.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanedDir, removed, err := CleanTempDirectory(filepath.Join(dir, "assets"), tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if cleanedDir != tempDir {
		t.Fatalf("expected cleaned temp dir %q, got %q", tempDir, cleanedDir)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed entries, got %d", removed)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("temp dir should remain readable: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir should be empty, got %d entries", len(entries))
	}
}

func TestCleanTempDirectoryRejectsEmptyPath(t *testing.T) {
	if _, _, err := CleanTempDirectory(t.TempDir(), ""); err == nil {
		t.Fatal("expected empty temp dir to be rejected")
	}
}

func TestCleanTempDirectoryRejectsStorageParent(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "assets")
	if _, _, err := CleanTempDirectory(storageDir, dir); err == nil {
		t.Fatal("expected storage parent temp dir to be rejected")
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
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("validated download should replace stale target: %+v", result)
	}
	data, err := os.ReadFile(finalPath)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("target was not replaced correctly data=%q err=%v", string(data), err)
	}
}

func TestMoveAssetFileReplacesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.tmp")
	dst := filepath.Join(dir, "asset.zip")
	if err := os.WriteFile(src, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveAssetFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "fresh" {
		t.Fatalf("target not replaced data=%q err=%v", string(data), err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("source temp file should be moved, err=%v", err)
	}
}

func TestMoveAssetFileRestoresTargetWhenReplacementRenameFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.tmp")
	dst := filepath.Join(dir, "asset.zip")
	if err := os.WriteFile(src, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRename := assetRename
	assetRename = func(from, to string) error {
		if from == src && to == dst {
			return errors.New("force copy fallback")
		}
		if strings.HasPrefix(filepath.Base(from), ".asset-") && to == dst {
			return errors.New("force replacement failure")
		}
		return oldRename(from, to)
	}
	t.Cleanup(func() { assetRename = oldRename })

	if err := moveAssetFile(src, dst); err == nil {
		t.Fatal("replacement failure should be returned")
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "stale" {
		t.Fatalf("old target should be restored data=%q err=%v", string(data), err)
	}
	if data, err := os.ReadFile(src); err != nil || string(data) != "fresh" {
		t.Fatalf("source should remain for retry data=%q err=%v", string(data), err)
	}
}

func TestDownloadSupersedesOldLocalAssetOnSamePath(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	rel := filepath.Join("p1", "v1", "a.zip")
	_, err := db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('old-asset', ?, ?, 6, 'old', 'verified')`, rel, digest("oldold"))
	if err != nil {
		t.Fatal(err)
	}
	primary := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abcdef"))
	}))
	defer primary.Close()
	task := fallbackTask(primary.URL, "", digest("abcdef"), 6)
	task.FallbackSources = nil
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: primary.Client(),
		AllowPrivateSourceURLs: true}).download(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("download should succeed: %+v", result)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'old-asset'`).Scan(&state)
	if err != nil || state != "superseded" {
		t.Fatalf("old same-path asset should be superseded state=%q err=%v", state, err)
	}
}
