package syncer

import (
	"os"
	"path/filepath"
)

type DirectoryCleanupStats struct {
	ProjectsScanned            int
	VersionDirectoriesRemoved  int
	ProjectDirectoriesRemoved  int
	NonEmptyDirectoriesSkipped int
	SymlinksSkipped            int
}

// CleanEmptyAssetDirectories repairs empty directories left by asset deletes
// that happened before per-delete directory cleanup was introduced.
func CleanEmptyAssetDirectories(storage string) (DirectoryCleanupStats, error) {
	var stats DirectoryCleanupStats
	projects, err := os.ReadDir(storage)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	for _, project := range projects {
		if project.Type()&os.ModeSymlink != 0 {
			stats.SymlinksSkipped++
			continue
		}
		if !project.IsDir() {
			continue
		}
		stats.ProjectsScanned++
		projectPath := filepath.Join(storage, project.Name())
		if err := cleanProjectDirectories(projectPath, &stats); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func cleanProjectDirectories(projectPath string, stats *DirectoryCleanupStats) error {
	versions, err := os.ReadDir(projectPath)
	if err != nil {
		return err
	}
	for _, version := range versions {
		if version.Type()&os.ModeSymlink != 0 {
			stats.SymlinksSkipped++
			continue
		}
		if !version.IsDir() {
			continue
		}
		removed, err := removeDirectoryWhenEmpty(filepath.Join(projectPath, version.Name()))
		if err != nil {
			return err
		}
		if removed {
			stats.VersionDirectoriesRemoved++
		} else {
			stats.NonEmptyDirectoriesSkipped++
		}
	}
	removed, err := removeDirectoryWhenEmpty(projectPath)
	if removed {
		stats.ProjectDirectoriesRemoved++
	} else if err == nil {
		stats.NonEmptyDirectoriesSkipped++
	}
	return err
}

// removeEmptyAssetDirectories removes empty parents from the asset's physical
// directory back toward the storage root. Modern assets may have extra internal
// identity directories, while legacy assets still use project/version/file.
func removeEmptyAssetDirectories(storage, relativeFile string) error {
	clean := filepath.Clean(relativeFile)
	if filepath.IsAbs(clean) || clean == "." || relEscapes(clean) {
		return nil
	}
	root, err := filepath.Abs(filepath.Clean(storage))
	if err != nil {
		return nil
	}
	assetPath, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil || assetPath == root || !sameOrInside(assetPath, root) {
		return nil
	}
	for dir := filepath.Dir(assetPath); dir != root && sameOrInside(dir, root); dir = filepath.Dir(dir) {
		removed, err := removeDirectoryWhenEmpty(dir)
		if err != nil {
			return err
		}
		if !removed {
			return nil
		}
	}
	return nil
}

func removeDirectoryWhenEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || len(entries) > 0 {
		return false, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}
