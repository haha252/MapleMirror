package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

func (s Server) syncScans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createScan(w, r)
	case http.MethodGet:
		if strings.HasSuffix(r.URL.Path, "/latest") {
			s.latestScan(w, r)
			return
		}
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
	}
}

func (s Server) createScan(w http.ResponseWriter, r *http.Request) {
	if s.Sync == nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "扫描服务未启用")
		return
	}
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	var body struct {
		ProjectID string `json:"project_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	scanID, err := s.Sync.Trigger(r.Context(), body.ProjectID, requestID(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "创建扫描任务失败")
		return
	}
	writeOK(w, r, http.StatusAccepted, "Release 扫描任务已创建",
		map[string]any{"scan_id": scanID, "state": "pending"})
}

func (s Server) latestScan(w http.ResponseWriter, r *http.Request) {
	if s.SyncStore.DB == nil {
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", "扫描服务未启用")
		return
	}
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	item, err := s.SyncStore.LatestScan(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "扫描记录不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "最近扫描", item)
}

func (s Server) syncStatus(w http.ResponseWriter, r *http.Request, nodeID string) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	item, err := s.SyncStore.SyncStatus(r.Context(), nodeID)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "同步状态不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "节点同步状态", item)
}

func (s Server) syncTaskAction(w http.ResponseWriter, r *http.Request, nodeID, action string) {
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	taskID, op := splitTaskAction(action)
	var err error
	switch op {
	case "retry":
		err = s.SyncStore.RetryTask(r.Context(), nodeID, taskID)
	case "cancel":
		err = s.SyncStore.CancelTask(r.Context(), nodeID, taskID)
	default:
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "接口不存在")
		return
	}
	if err == sql.ErrNoRows {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "同步任务不存在")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "同步任务操作失败")
		return
	}
	writeOK(w, r, http.StatusOK, "同步任务已更新",
		map[string]any{"node_id": nodeID, "task_id": taskID})
}

func splitTaskAction(action string) (string, string) {
	rest := strings.TrimPrefix(action, "sync-tasks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

type ScanTrigger interface {
	Trigger(context.Context, string, string) (string, error)
}
