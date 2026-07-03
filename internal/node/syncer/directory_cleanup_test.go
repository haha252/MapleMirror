package syncer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCleanEmptyAssetDirectoriesRepairsHistoricalDirectories(t *testing.T) {
	storage := t.TempDir()
	mustMakeDir(t, filepath.Join(storage, "empty-project"))
	mustMakeDir(t, filepath.Join(storage, "old-project", "v1"))
	mustMakeDir(t, filepath.Join(storage, "active-project", "old"))
	activeVersion := filepath.Join(storage, "active-project", "current")
	mustMakeDir(t, activeVersion)
	mustWriteFile(t, filepath.Join(activeVersion, "asset.zip"))

	stats, err := CleanEmptyAssetDirectories(storage)
	if err != nil {
		t.Fatal(err)
	}
	assertPathMissing(t, filepath.Join(storage, "empty-project"))
	assertPathMissing(t, filepath.Join(storage, "old-project"))
	assertPathMissing(t, filepath.Join(storage, "active-project", "old"))
	assertPathExists(t, filepath.Join(activeVersion, "asset.zip"))
	assertPathExists(t, storage)
	if stats.ProjectsScanned != 3 || stats.VersionDirectoriesRemoved != 2 ||
		stats.ProjectDirectoriesRemoved != 2 {
		t.Fatalf("unexpected cleanup stats: %+v", stats)
	}
}

func TestCleanEmptyAssetDirectoriesKeepsHiddenFilesAndSymlinks(t *testing.T) {
	storage := t.TempDir()
	version := filepath.Join(storage, "project", "version")
	mustMakeDir(t, version)
	mustWriteFile(t, filepath.Join(version, ".keep"))
	if runtime.GOOS != "windows" {
		if err := os.Symlink(t.TempDir(), filepath.Join(storage, "linked-project")); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := CleanEmptyAssetDirectories(storage)
	if err != nil {
		t.Fatal(err)
	}
	assertPathExists(t, filepath.Join(version, ".keep"))
	if stats.NonEmptyDirectoriesSkipped < 2 {
		t.Fatalf("non-empty directories were not counted: %+v", stats)
	}
	if runtime.GOOS != "windows" && stats.SymlinksSkipped != 1 {
		t.Fatalf("symlink was not skipped: %+v", stats)
	}
}

func TestCleanEmptyAssetDirectoriesDoesNotRemoveStorageRoot(t *testing.T) {
	storage := t.TempDir()
	stats, err := CleanEmptyAssetDirectories(storage)
	if err != nil {
		t.Fatal(err)
	}
	assertPathExists(t, storage)
	if stats != (DirectoryCleanupStats{}) {
		t.Fatalf("empty storage should have zero stats: %+v", stats)
	}
}

func mustMakeDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("path should exist %s: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path should be absent %s: %v", path, err)
	}
}
