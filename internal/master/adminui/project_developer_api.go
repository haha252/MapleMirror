package adminui

import (
	"fmt"
	"net/http"

	"mirror-server/internal/config"
)

func (s *Server) projectDeveloperAPIInfo(w http.ResponseWriter, r *http.Request, projectID string) {
	w.Header().Set("Cache-Control", "no-store")
	if s.developerAPI == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"message": "Developer API 服务未启用"})
		return
	}
	if !s.projectExists(projectID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "项目不存在"})
		return
	}
	info, err := s.developerAPI.Info(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"message": "Developer API 信息查询失败"})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) rotateProjectDeveloperToken(w http.ResponseWriter,
	r *http.Request, projectID string) {
	w.Header().Set("Cache-Control", "no-store")
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if s.developerAPI == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"message": "Developer API 服务未启用"})
		return
	}
	if !s.projectExists(projectID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "项目不存在"})
		return
	}
	result, err := s.developerAPI.RotateToken(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			map[string]string{"message": "Developer API Token 生成失败"})
		return
	}
	_ = s.repo.Audit(r.Context(), "project.developer_token.rotate", "project",
		projectID, "success", requestID(r),
		fmt.Sprintf("Developer API Token 已生成或重置，前缀 %s", result.TokenPrefix), admin)
	writeJSON(w, http.StatusCreated, map[string]any{
		"message":       "Developer API Token 已生成；完整 Token 仅在本次响应中返回",
		"developer_api": result.APIInfo,
		"token":         result.Token,
	})
}

func (s *Server) projectExists(projectID string) bool {
	projects := config.Projects{}
	if s.projects != nil {
		var err error
		projects, err = s.projects.Load()
		if err != nil {
			projects = s.projects.Current()
		}
	}
	for _, project := range projects.Projects {
		if project.ID == projectID {
			return true
		}
	}
	return false
}
