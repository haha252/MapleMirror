package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

var checkedExtensions = map[string]bool{
	".go": true, ".sql": true, ".yaml": true, ".yml": true,
	".ps1": true, ".bat": true, ".sh": true, ".js": true,
	".css": true, ".html": true, ".md": true, ".mod": true,
	".sum": true, ".gitattributes": true, ".gitignore": true,
}

func TestTextFilesAreUTF8AndMaintainedFilesStaySmall(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	allowLong := map[string]bool{
		filepath.Clean("configs/config.example.yaml"):                   true,
		filepath.Clean("internal/config/templates/config.example.yaml"): true,
	}
	translationDirectory := filepath.Join("web", "public", "static", "i18n") + string(os.PathSeparator)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if entry.IsDir() && skippedDirectory(relative) {
			return filepath.SkipDir
		}
		extension := strings.ToLower(filepath.Ext(path))
		if entry.IsDir() || !checkedExtensions[extension] {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(content) {
			t.Errorf("文本文件不是有效 UTF-8：%s", relative)
		}
		if extension != ".md" && countLines(content) > 250 && !allowLong[relative] && !strings.HasPrefix(relative, translationDirectory) {
			t.Errorf("非 Markdown 文件超过 250 行：%s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func skippedDirectory(relative string) bool {
	return relative == ".git" || relative == ".cache" || relative == "dist"
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := strings.Count(string(content), "\n")
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}
