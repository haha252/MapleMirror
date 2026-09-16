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

func TestCleanTempDirectoryPreservesSwarmPartials(t *testing.T) {
	dir := t.TempDir()
	tempDir := filepath.Join(dir, "tmp")
	partialDir := filepath.Join(tempDir, "swarm-partials")
	if err := os.MkdirAll(partialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(partialDir, "resume.part")
	if err := os.WriteFile(partial, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "stale.tmp"), []byte("remove"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, removed, err := CleanTempDirectory(filepath.Join(dir, "assets"), tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d want=1", removed)
	}
	if data, err := os.ReadFile(partial); err != nil || string(data) != "keep" {
		t.Fatalf("swarm partial should survive startup cleanup data=%q err=%v", data, err)
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

func TestDownloadDoesNotReuseUntrackedCanonicalFile(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	canonical := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("partial"), 0o600); err != nil {
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
		t.Fatalf("validated download should succeed: %+v", result)
	}
	if data, err := os.ReadFile(canonical); err != nil || string(data) != "partial" {
		t.Fatalf("untracked canonical file should be left untouched data=%q err=%v", string(data), err)
	}
	physical := filepath.Join(storageDir, relativeAssetPath(task.Asset))
	if data, err := os.ReadFile(physical); err != nil || string(data) != "abcdef" {
		t.Fatalf("asset should use identity-isolated path data=%q err=%v", string(data), err)
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

func TestDownloadKeepsOlderSamePublicPathAssetVerified(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	oldRel := filepath.Join("p1", "v1", ".mirror-assets", "old", "a.zip")
	oldPath := filepath.Join(storageDir, oldRel)
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("oldold"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('old-asset', ?, ?, 6, 'old', 'verified')`, oldRel, digest("oldold"))
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
	var oldState, newState, newRel string
	if err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id='old-asset'`).Scan(&oldState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state,relative_path FROM local_assets WHERE asset_id='asset-1'`).Scan(&newState, &newRel); err != nil {
		t.Fatal(err)
	}
	if oldState != "verified" || newState != "verified" || newRel == oldRel {
		t.Fatalf("same public path generations must coexist old=%s new=%s oldRel=%q newRel=%q", oldState, newState, oldRel, newRel)
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "oldold" {
		t.Fatalf("older generation should remain on disk data=%q err=%v", string(data), err)
	}
}
