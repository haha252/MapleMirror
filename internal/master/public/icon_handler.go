package public

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mirror-server/internal/config"
)

type projectAssetConfig struct {
	IconPath                   string
	ArchitectureDefaultEnabled bool
	SystemMatchEnabled         bool
}

func (s Server) projectIcon(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimPrefix(r.URL.Path, "/static/project-icons/")
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.Contains(projectID, "/") {
		http.NotFound(w, r)
		return
	}
	assets, err := s.assets()
	if err != nil {
		http.Error(w, "静态资源读取失败", http.StatusInternalServerError)
		return
	}
	project, ok := s.currentProjectAssets()[projectID]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.TrimSpace(project.IconPath) == "" {
		writeProjectIcon(w, assets.placeholder, ".svg")
		return
	}
	data, err := os.ReadFile(project.IconPath)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(context.Background(), "项目图标读取失败，已回退占位图标",
				slog.String("project_id", projectID),
				slog.String("icon_path", project.IconPath),
				slog.String("error", err.Error()))
		}
		writeProjectIcon(w, assets.placeholder, ".svg")
		return
	}
	writeProjectIcon(w, data, filepath.Ext(project.IconPath))
}

func (s Server) currentProjectAssets() map[string]projectAssetConfig {
	if strings.TrimSpace(s.ProjectsPath) == "" {
		return s.ProjectAssets
	}
	projects, err := config.LoadProjects(s.ProjectsPath, nil)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(context.Background(), "项目清单热重载失败，项目图标沿用上一次有效配置",
				slog.String("error", err.Error()))
		}
		return s.ProjectAssets
	}
	return projectAssetMap(projects)
}

func writeProjectIcon(w http.ResponseWriter, data []byte, extension string) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	switch strings.ToLower(extension) {
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	default:
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	}
	_, _ = w.Write(data)
}

func (s Server) favicon(w http.ResponseWriter, r *http.Request) {
	assets, err := s.assets()
	if err != nil {
		noContent(w, r)
		return
	}
	data, err := assets.readStatic("logo.webp")
	if err != nil {
		noContent(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "image/webp")
	_, _ = w.Write(data)
}

func (assets *webAssets) readStatic(name string) ([]byte, error) {
	if assets.staticFS != nil {
		return fs.ReadFile(assets.staticFS, name)
	}
	return os.ReadFile(filepath.Join(assets.staticDir, name))
}
