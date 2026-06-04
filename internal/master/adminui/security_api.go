package adminui

import (
	"net/http"
	"strings"
)

func (s *Server) securityBlocksAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	adminBlocks, err := s.listAdminBlocks(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "登录封禁查询失败"})
		return
	}
	clientBlocks, err := s.listClientBlocks(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "下载封禁查询失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"admin_blocks":  adminBlocks,
		"client_blocks": clientBlocks,
	})
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
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "封禁记录不存在"})
		return
	}
	switch kind {
	case "admin":
		_, _ = s.repo.DB.ExecContext(r.Context(), `DELETE FROM admin_ip_blocks WHERE ip_key = ?`, id)
	case "client":
		_, _ = s.repo.DB.ExecContext(r.Context(), `DELETE FROM client_blocks WHERE client_prefix_key = ?`, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "封禁已解除"})
}

func (s *Server) listAdminBlocks(r *http.Request) ([]map[string]any, error) {
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT ip_key, masked_ip,
		reason, blocked_at, expires_at, attempts_after_block, last_attempt_at
		FROM admin_ip_blocks WHERE expires_at > ? ORDER BY blocked_at DESC LIMIT 100`, nowText())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var key, masked, reason, blocked, expires, last string
		var attempts int
		if err := rows.Scan(&key, &masked, &reason, &blocked, &expires, &attempts, &last); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"key": key, "masked_ip": masked,
			"reason": reason, "blocked_at": blocked, "expires_at": expires,
			"attempts_after_block": attempts, "last_attempt_at": last})
	}
	return items, rows.Err()
}

func (s *Server) listClientBlocks(r *http.Request) ([]map[string]any, error) {
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT client_prefix_key,
		reason, source, blocked_at, expires_at, attempts_after_block, last_attempt_at
		FROM client_blocks WHERE expires_at > ? ORDER BY blocked_at DESC LIMIT 100`, nowText())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var key, reason, source, blocked, expires, last string
		var attempts int
		if err := rows.Scan(&key, &reason, &source, &blocked, &expires, &attempts, &last); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"key": key, "masked_ip": maskBlockKey(key),
			"reason": reason, "source": source, "blocked_at": blocked,
			"expires_at": expires, "attempts_after_block": attempts, "last_attempt_at": last})
	}
	return items, rows.Err()
}

func maskBlockKey(value string) string {
	if len(value) <= 10 {
		return value
	}
	return strings.TrimSpace(value[:10]) + "*"
}
