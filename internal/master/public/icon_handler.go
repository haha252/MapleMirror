package public

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type projectAssetConfig struct {
	IconPath string
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
	project, ok := s.ProjectAssets[projectID]
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
	data, err := os.ReadFile(filepath.Join(assets.staticDir, "logo.webp"))
	if err != nil {
		noContent(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Type", "image/webp")
	_, _ = w.Write(data)
}
