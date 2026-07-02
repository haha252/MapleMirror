package syncer

import (
	"os"
	"path/filepath"
	"strings"
)

// removeEmptyAssetDirectories removes the version directory first and then
// its project directory. It never removes the storage root and leaves any
// non-empty directory untouched.
func removeEmptyAssetDirectories(storage, relativeFile string) error {
	clean := filepath.Clean(relativeFile)
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if filepath.IsAbs(clean) || len(parts) != 3 || clean == "." || relEscapes(clean) {
		return nil
	}
	versionDir := filepath.Join(storage, parts[0], parts[1])
	projectDir := filepath.Join(storage, parts[0])
	if err := removeDirectoryWhenEmpty(versionDir); err != nil {
		return err
	}
	return removeDirectoryWhenEmpty(projectDir)
}

func removeDirectoryWhenEmpty(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || len(entries) > 0 {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
