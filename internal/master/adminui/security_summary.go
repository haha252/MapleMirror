package adminui

import (
	"net/http"
	"time"
)

func (s *Server) securitySummaryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	now := nowText()
	windowStart := time.Now().UTC().Add(-s.store.window).Format(time.RFC3339Nano)
	dayStart := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	count := func(query string, args ...any) (int, error) {
		var value int
		err := s.repo.DB.QueryRowContext(r.Context(), query, args...).Scan(&value)
		return value, err
	}
	adminBlocks, err := count("SELECT COUNT(*) FROM admin_ip_blocks WHERE expires_at > ?", now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	clientBlocks, err := count("SELECT COUNT(*) FROM client_blocks WHERE expires_at > ?", now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	punishment, err := count("SELECT COUNT(*) FROM client_blocks WHERE expires_at > ? AND punishment_active = 1", now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	sessions, err := count("SELECT COUNT(*) FROM admin_web_sessions WHERE expires_at > ?", now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	loginSources, err := count("SELECT COUNT(*) FROM admin_login_failures WHERE last_failed_at > ?", windowStart)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	failedOps, err := count("SELECT COUNT(*) FROM admin_audit_events WHERE created_at > ? AND LOWER(result) NOT IN ('success', 'succeeded', 'ok')", dayStart)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "安全概览查询失败"})
		return
	}
	warnings, err := s.loginFailureWarnings(r, windowStart)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "登录异常查询失败"})
		return
	}
	attention := len(warnings)
	if punishment > 0 {
		attention++
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"attention_count":          attention,
		"active_blocks":            adminBlocks + clientBlocks,
		"admin_blocks":             adminBlocks,
		"client_blocks":            clientBlocks,
		"login_failure_sources":    loginSources,
		"punishment_active":        punishment,
		"active_sessions":          sessions,
		"failed_admin_actions_24h": failedOps,
		"login_failure_limit":      s.store.limit,
		"login_warnings":           warnings,
	})
}

func (s *Server) loginFailureWarnings(r *http.Request, windowStart string) ([]map[string]any, error) {
	threshold := s.store.limit - 1
	if threshold < 1 {
		threshold = 1
	}
	query := "SELECT masked_ip, failed_count, last_failed_at FROM admin_login_failures " +
		"WHERE last_failed_at > ? AND failed_count >= ? " +
		"ORDER BY failed_count DESC, last_failed_at DESC LIMIT 5"
	rows, err := s.repo.DB.QueryContext(r.Context(), query, windowStart, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var ip, last string
		var failures int
		if err := rows.Scan(&ip, &failures, &last); err != nil {
			return nil, err
		}
		remaining := s.store.limit - failures
		if remaining < 0 {
			remaining = 0
		}
		items = append(items, map[string]any{
			"masked_ip":              ip,
			"failed_count":           failures,
			"remaining_before_block": remaining,
			"last_failed_at":         s.displayTime(last),
		})
	}
	return items, rows.Err()
}
