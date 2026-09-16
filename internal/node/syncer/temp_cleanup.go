package syncer

import (
	"fmt"
	"os"
	"path/filepath"
)

func CleanTempDirectory(storageDir, tempDir string) (string, int, error) {
	dir := effectiveTempDir(storageDir, tempDir)
	if err := ensureSafeTempDirectory(storageDir, dir); err != nil {
		return dir, 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.Name() == "swarm-partials" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return dir, removed, err
		}
		removed++
	}
	return dir, removed, nil
}

func ensureSafeTempDirectory(storageDir, dir string) error {
	if dir == "" {
		return fmt.Errorf("临时目录不能为空")
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cwdAbs, err := filepath.Abs(filepath.Clean(cwd))
	if err != nil {
		return err
	}
	if filepath.Dir(abs) == abs {
		return fmt.Errorf("拒绝清理根目录: %s", abs)
	}
	if abs == cwdAbs {
		return fmt.Errorf("拒绝清理当前工作目录: %s", abs)
	}
	storageAbs, err := filepath.Abs(filepath.Clean(storageDir))
	if err != nil {
		return err
	}
	if sameOrInside(storageAbs, abs) {
		return fmt.Errorf("拒绝清理存储目录或其父目录: %s", abs)
	}
	return nil
}
