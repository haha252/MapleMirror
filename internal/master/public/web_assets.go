package public

import (
	"errors"
	"html/template"
	"os"
	"path/filepath"
	"sync"
)

type webAssets struct {
	templateDir  string
	staticDir    string
	pageTemplate *template.Template
	downloadTmpl *template.Template
	placeholder  []byte
}

var (
	defaultAssets     *webAssets
	defaultAssetsErr  error
	defaultAssetsOnce sync.Once
)

func loadDefaultWebAssets() (*webAssets, error) {
	defaultAssetsOnce.Do(func() {
		root, err := findRepoResource("web", "public")
		if err != nil {
			defaultAssetsErr = err
			return
		}
		defaultAssets, defaultAssetsErr = loadWebAssets(root)
	})
	return defaultAssets, defaultAssetsErr
}

func loadWebAssets(root string) (*webAssets, error) {
	templateDir := filepath.Join(root, "templates")
	staticDir := filepath.Join(root, "static")
	pageTemplate, err := template.ParseFiles(filepath.Join(templateDir, "page.html"))
	if err != nil {
		return nil, err
	}
	downloadTmpl, err := template.ParseFiles(filepath.Join(templateDir, "download.html"))
	if err != nil {
		return nil, err
	}
	placeholder, err := os.ReadFile(filepath.Join(staticDir, "placeholder-project.svg"))
	if err != nil {
		return nil, err
	}
	return &webAssets{
		templateDir:  templateDir,
		staticDir:    staticDir,
		pageTemplate: pageTemplate,
		downloadTmpl: downloadTmpl,
		placeholder:  placeholder,
	}, nil
}

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
