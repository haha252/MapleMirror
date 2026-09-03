package public

import (
	"errors"
	"os"
	"path/filepath"
)

func findRepoResource(parts ...string) (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, start := range candidates {
		for dir := start; ; dir = filepath.Dir(dir) {
			target := filepath.Join(append([]string{dir}, parts...)...)
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				return target, nil
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
		}
	}
	return "", errors.New("未找到 web/public 资源目录")
}
