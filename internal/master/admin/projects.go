package admin

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"mirror-server/internal/config"
)

func (s Server) projectByID(w http.ResponseWriter, r *http.Request) {
	projectID, action := splitProjectPath(r.URL.Path)
	if projectID == "" {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "项目不存在")
		return
	}
	switch {
	case r.Method == http.MethodPost && action == "reset":
		s.resetProject(w, r, projectID)
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
	}
}

func (s Server) resetProject(w http.ResponseWriter, r *http.Request, projectID string) {
	if s.Sync == nil || s.SyncStore.DB == nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "扫描服务未启用")
		return
	}
	adminID, ok := s.require(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "项目数据已重置并重新扫描"
	}
	if !s.projectEnabled(r, projectID) {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "项目不存在")
		return
	}
	if err := s.SyncStore.ResetProject(r.Context(), projectID); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "项目不存在")
			return
		}
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "项目重置失败",
				slog.String("request_id", requestID(r)),
				slog.String("project_id", projectID),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "项目重置失败")
		return
	}
	scanID, err := s.Sync.Trigger(r.Context(), projectID, requestID(r))
	if err != nil {
		_ = s.Repo.Audit(r.Context(), "project.reset", "project", projectID, "failed", requestID(r), "项目数据已清空，重新扫描失败："+reason, adminID)
		if s.Logger != nil {
			s.Logger.Warn(r.Context(), "项目重置后重新扫描失败",
				slog.String("request_id", requestID(r)),
				slog.String("project_id", projectID),
				slog.String("error", err.Error()))
		}
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "项目重置后重新扫描失败")
		return
	}
	_ = s.Repo.Audit(r.Context(), "project.reset", "project", projectID, "success", requestID(r), reason, adminID)
	if s.Logger != nil {
		s.Logger.Debug(r.Context(), "项目已重置并重新扫描",
			slog.String("request_id", requestID(r)),
			slog.String("project_id", projectID),
			slog.String("scan_id", scanID))
	}
	writeOK(w, r, http.StatusOK, "项目数据已重置并重新扫描", map[string]any{
		"project_id": projectID,
		"scan_id":    scanID,
	})
}

func (s Server) projectEnabled(r *http.Request, projectID string) bool {
	if s.Projects == nil {
		return true
	}
	projects, err := s.Projects.Load()
	if err != nil && s.Logger != nil {
		s.Logger.Warn(r.Context(), "项目清单热重载失败，沿用上一次有效配置",
			slog.String("error", err.Error()))
		projects = s.Projects.Current()
	}
	project, ok := lookupProject(projects, projectID)
	return ok && project.Enabled
}

func lookupProject(projects config.Projects, projectID string) (config.Project, bool) {
	for _, project := range projects.Projects {
		if project.ID == projectID {
			return project, true
		}
	}
	return config.Project{}, false
}

func splitProjectPath(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/admin/v1/projects/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], "/")
}
