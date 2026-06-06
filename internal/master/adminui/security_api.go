package adminui

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func (s *Server) securityBlocksAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listBlocksAPI(w, r)
	case http.MethodPost:
		s.createBlockAPI(w, r)
	case http.MethodDelete:
		s.deleteBlocksAPI(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
	}
}

func (s *Server) listBlocksAPI(w http.ResponseWriter, r *http.Request) {
	page := paginationFrom(r, 20)
	items, total, err := s.listBlocks(r, page)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "封禁列表查询失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{"blocks": items, "pagination": page})
}

func (s *Server) createBlockAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	var body struct {
		Kind     string `json:"kind"`
		Key      string `json:"key"`
		Reason   string `json:"reason"`
		Duration string `json:"duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "请求内容不合法"})
		return
	}
	if err := s.createBlock(r, body.Kind, body.Key, body.Reason, body.Duration); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"message": "封禁已添加"})
}

func (s *Server) deleteBlocksAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	var body struct {
		Items []struct {
			Kind string `json:"kind"`
			Key  string `json:"key"`
		} `json:"items"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	deleted := 0
	for _, item := range body.Items {
		if err := s.deleteBlock(r, item.Kind, item.Key); err == nil {
			deleted++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "封禁已解除", "deleted": deleted})
}

func (s *Server) securityBlockActionAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	if _, ok := s.requireHighRisk(w, r); !ok {
		return
	}
	kind, id := splitAdminPath(r.URL.Path, "/admin/api/security/blocks/")
	if err := s.deleteBlock(r, kind, id); err != nil {
		writeDeleteBlockError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "封禁已解除"})
}

func writeDeleteBlockError(w http.ResponseWriter, err error) {
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "封禁记录不存在"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "封禁解除失败"})
}
