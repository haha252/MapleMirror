package adminui

import (
	"fmt"
	"net/http"
)

func (s *Server) indexNowAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	admin, ok := s.requireHighRisk(w, r)
	if !ok {
		return
	}
	if s.indexNow == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "IndexNow 通知未启用"})
		return
	}
	queued, err := s.indexNow.TriggerFullPublicNotification(r.Context())
	if err != nil {
		_ = s.repo.Audit(r.Context(), "indexnow.submit", "site", "public", "failure",
			requestID(r), "IndexNow 全量提交失败："+err.Error(), admin)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "IndexNow 全量提交失败"})
		return
	}
	_ = s.repo.Audit(r.Context(), "indexnow.submit", "site", "public", "queued",
		requestID(r), fmt.Sprintf("IndexNow 全量 URL 已立即排队：%d 条", queued), admin)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"message":   "IndexNow 全量 URL 已立即排队",
		"url_count": queued,
	})
}
