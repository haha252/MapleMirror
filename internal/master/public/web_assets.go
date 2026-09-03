package public

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"mirror-server/web"
)

type webAssets struct {
	templateDir       string
	staticDir         string
	staticFS          fs.FS
	pageTemplate      *template.Template
	downloadTmpl      *template.Template
	downloadPowTmpl   *template.Template
	blockedTmpl       *template.Template
	punishmentPowTmpl *template.Template
	projectTmpl       *template.Template
	placeholder       []byte
	staticManifest    map[string]string
	staticJSON        template.JS
	localeMessages    map[string]map[string]string
}

var (
	defaultAssets     *webAssets
	defaultAssetsErr  error
	defaultAssetsOnce sync.Once
)

func loadDefaultWebAssets() (*webAssets, error) {
	defaultAssetsOnce.Do(func() {
		defaultAssets, defaultAssetsErr = loadEmbeddedWebAssets()
	})
	return defaultAssets, defaultAssetsErr
}

func loadEmbeddedWebAssets() (*webAssets, error) {
	staticFS, err := fs.Sub(web.Assets, "public/static")
	if err != nil {
		return nil, err
	}
	manifest, staticJSON, err := buildStaticManifest(staticFS)
	if err != nil {
		return nil, err
	}
	localeMessages, err := loadPublicLocaleMessages(staticFS)
	if err != nil {
		return nil, err
	}
	funcs := template.FuncMap{"staticURL": staticURLFunc(manifest)}
	pageTemplate, err := template.New("page.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/page.html")
	if err != nil {
		return nil, err
	}
	downloadTmpl, err := template.New("download.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/download.html")
	if err != nil {
		return nil, err
	}
	downloadPowTmpl, err := template.New("download_pow.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/download_pow.html")
	if err != nil {
		return nil, err
	}
	blockedTmpl, err := template.New("blocked.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/blocked.html")
	if err != nil {
		return nil, err
	}
	punishmentPowTmpl, err := template.New("punishment_pow.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/punishment_pow.html")
	if err != nil {
		return nil, err
	}
	projectTmpl, err := template.New("project.html").Funcs(funcs).ParseFS(web.Assets, "public/templates/project.html")
	if err != nil {
		return nil, err
	}
	placeholder, err := fs.ReadFile(staticFS, "placeholder-project.svg")
	if err != nil {
		return nil, err
	}
	return &webAssets{
		staticFS:          staticFS,
		pageTemplate:      pageTemplate,
		downloadTmpl:      downloadTmpl,
		downloadPowTmpl:   downloadPowTmpl,
		blockedTmpl:       blockedTmpl,
		punishmentPowTmpl: punishmentPowTmpl,
		projectTmpl:       projectTmpl,
		placeholder:       placeholder,
		staticManifest:    manifest,
		staticJSON:        staticJSON,
		localeMessages:    localeMessages,
	}, nil
}

func loadDefaultWebAssetsFromDisk() (*webAssets, error) {
	root, err := findRepoResource("web", "public")
	if err != nil {
		return nil, err
	}
	return loadWebAssets(root)
}

func loadWebAssets(root string) (*webAssets, error) {
	templateDir := filepath.Join(root, "templates")
	staticDir := filepath.Join(root, "static")
	staticFS := os.DirFS(staticDir)
	manifest, staticJSON, err := buildStaticManifest(staticFS)
	if err != nil {
		return nil, err
	}
	localeMessages, err := loadPublicLocaleMessages(staticFS)
	if err != nil {
		return nil, err
	}
	funcs := template.FuncMap{"staticURL": staticURLFunc(manifest)}
	pageTemplate, err := template.New("page.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "page.html"))
	if err != nil {
		return nil, err
	}
	downloadTmpl, err := template.New("download.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "download.html"))
	if err != nil {
		return nil, err
	}
	downloadPowTmpl, err := template.New("download_pow.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "download_pow.html"))
	if err != nil {
		return nil, err
	}
	blockedTmpl, err := template.New("blocked.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "blocked.html"))
	if err != nil {
		return nil, err
	}
	punishmentPowTmpl, err := template.New("punishment_pow.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "punishment_pow.html"))
	if err != nil {
		return nil, err
	}
	projectTmpl, err := template.New("project.html").Funcs(funcs).ParseFiles(filepath.Join(templateDir, "project.html"))
	if err != nil {
		return nil, err
	}
	placeholder, err := os.ReadFile(filepath.Join(staticDir, "placeholder-project.svg"))
	if err != nil {
		return nil, err
	}
	return &webAssets{
		templateDir:       templateDir,
		staticDir:         staticDir,
		pageTemplate:      pageTemplate,
		downloadTmpl:      downloadTmpl,
		downloadPowTmpl:   downloadPowTmpl,
		blockedTmpl:       blockedTmpl,
		punishmentPowTmpl: punishmentPowTmpl,
		projectTmpl:       projectTmpl,
		placeholder:       placeholder,
		staticManifest:    manifest,
		staticJSON:        staticJSON,
		localeMessages:    localeMessages,
	}, nil
}

func buildStaticManifest(staticFS fs.FS) (map[string]string, template.JS, error) {
	names := make([]string, 0)
	err := fs.WalkDir(staticFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != "." && entry.Type().IsRegular() {
			names = append(names, strings.TrimPrefix(path, "./"))
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(names)
	manifest := make(map[string]string, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(staticFS, name)
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(data)
		manifest[name] = "/static/public/" + name + "?v=" + hex.EncodeToString(sum[:])[:12]
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", err
	}
	return manifest, template.JS(data), nil
}

func staticURLFunc(manifest map[string]string) func(string) string {
	return func(raw string) string {
		name := strings.TrimSpace(raw)
		if name == "" {
			return ""
		}
		name = strings.TrimPrefix(name, "/static/public/")
		fragment := ""
		if before, after, ok := strings.Cut(name, "#"); ok {
			name = before
			fragment = "#" + after
		}
		query := ""
		if before, after, ok := strings.Cut(name, "?"); ok {
			name = before
			query = "?" + after
		}
		if versioned, ok := manifest[name]; ok {
			return versioned + fragment
		}
		return "/static/public/" + name + query + fragment
	}
}

func (assets *webAssets) staticJSONFor(names []string) template.JS {
	if names == nil {
		return assets.staticJSON
	}
	manifest := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := assets.staticManifest[name]; ok {
			manifest[name] = value
		}
	}
	data, _ := json.Marshal(manifest)
	return template.JS(data)
}
