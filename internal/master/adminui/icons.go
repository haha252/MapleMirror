package adminui

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) projectIcon(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/static/project-icons/"))
	if projectID == "" || strings.Contains(projectID, "/") {
		http.NotFound(w, r)
		return
	}
	iconPath := ""
	if s.projects != nil {
		for _, project := range s.projects.Current().Projects {
			if project.ID == projectID {
				iconPath = project.ResolvedIconPath
				break
			}
		}
	}
	if strings.TrimSpace(iconPath) != "" {
		if data, err := os.ReadFile(iconPath); err == nil {
			writeIcon(w, data, filepath.Ext(iconPath))
			return
		}
	}
	data, err := fs.ReadFile(s.publicFS, "placeholder-project.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeIcon(w, data, ".svg")
}

func writeIcon(w http.ResponseWriter, data []byte, ext string) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	switch strings.ToLower(ext) {
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
		w.Header().Set("Content-Security-Policy",
			"sandbox; script-src 'none'; object-src 'none'; base-uri 'none'")
	}
	_, _ = w.Write(data)
}
