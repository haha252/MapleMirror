package adminui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/config"

	"gopkg.in/yaml.v3"
)

func (s *Server) projectsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.projects == nil {
			writeJSON(w, http.StatusOK, config.Projects{})
			return
		}
		writeJSON(w, http.StatusOK, s.projects.Current())
	case http.MethodPut:
		s.saveProjects(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
	}
}

func (s *Server) saveProjects(w http.ResponseWriter, r *http.Request) {
	if s.projects == nil || s.projects.Path == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "项目配置路径未配置"})
		return
	}
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	var incoming config.Projects
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "项目配置 JSON 不合法"})
		return
	}
	if err := writeProjectsFile(s.projects.Path, incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	loaded, err := s.projects.Load()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if err := s.syncStore.SyncProjectConfig(r.Context(), loaded); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "项目配置已保存，同步运行状态更新失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "项目配置已保存", "projects": loaded.Projects})
}

func writeProjectsFile(path string, projects config.Projects) error {
	data, err := yaml.Marshal(projects)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, ".projects."+time.Now().UTC().Format("20060102150405")+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if _, err := config.LoadProjects(tmp, nil); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
