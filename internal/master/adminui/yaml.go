package adminui

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func readYAMLFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}

func dirName(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return "."
	}
	return dir
}
