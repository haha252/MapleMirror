package adminui

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"mirror-server/internal/master/assignment"
)

func (s *Server) nodeProjects(w http.ResponseWriter, r *http.Request, nodeID string) {
	switch r.Method {
	case http.MethodGet:
		s.readNodeProjects(w, r, nodeID)
	case http.MethodPut:
		s.saveNodeProjects(w, r, nodeID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
	}
}

func (s *Server) readNodeProjects(w http.ResponseWriter, r *http.Request, nodeID string) {
	data, err := assignment.ReadNodeProjects(r.Context(), s.repo.DB, nodeID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "节点项目分配查询失败"})
		return
	}
	for i := range data.Projects {
		data.Projects[i].LastChangedAt = s.displayTime(data.Projects[i].LastChangedAt)
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) saveNodeProjects(w http.ResponseWriter, r *http.Request, nodeID string) {
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	var body struct {
		AssignmentMode string   `json:"assignment_mode"`
		Projects       []string `json:"projects"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	err := assignment.SaveNodeProjects(r.Context(), s.repo.DB, nodeID,
		body.AssignmentMode, body.Projects, time.Now())
	if errors.Is(err, assignment.ErrManualLimitExceeded) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "手动分配项目数超过节点上限"})
		return
	}
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "节点不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "节点项目分配保存失败"})
		return
	}
	if s.syncStore.Runtime != nil {
		s.syncStore.Runtime.NotifySyncTasks(nodeID)
	}
	_ = s.repo.Audit(r.Context(), "node.projects.update", "node", nodeID,
		"success", requestID(r), "节点项目分配已更新", admin)
	writeJSON(w, http.StatusOK, map[string]any{"message": "节点项目分配已保存", "node_id": nodeID})
}
