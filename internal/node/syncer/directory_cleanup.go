package syncer

import (
	"os"
	"path/filepath"
	"strings"
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
	if _, err := removeDirectoryWhenEmpty(versionDir); err != nil {
		return err
	}
	_, err := removeDirectoryWhenEmpty(projectDir)
	return err
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
