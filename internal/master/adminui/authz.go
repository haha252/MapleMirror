package adminui

import "net/http"

func (s *Server) requireHighRisk(w http.ResponseWriter, r *http.Request) (string, bool) {
	username, _ := r.Context().Value(usernameKey{}).(string)
	if username == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "该操作需要管理面板登录会话"})
		return "", false
	}
	return username, true
}
