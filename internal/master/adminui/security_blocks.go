package adminui

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *Server) listBlocks(r *http.Request, page pagination) ([]map[string]any, int, error) {
	now := nowText()
	var total int
	err := s.repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM (
		SELECT ip_key FROM admin_ip_blocks WHERE expires_at > ?
		UNION ALL SELECT client_prefix_key FROM client_blocks WHERE expires_at > ?)`,
		now, now).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT kind, block_key,
		masked_ip, reason, source, blocked_at, expires_at, attempts_after_block,
		last_attempt_at FROM (
		SELECT 'admin' AS kind, ip_key AS block_key, masked_ip, reason,
		'管理登录' AS source, blocked_at, expires_at, attempts_after_block,
		last_attempt_at FROM admin_ip_blocks WHERE expires_at > ?
		UNION ALL
		SELECT 'client' AS kind, client_prefix_key AS block_key,
		client_prefix_key AS masked_ip, reason, source, blocked_at, expires_at,
		attempts_after_block, last_attempt_at FROM client_blocks WHERE expires_at > ?
		) ORDER BY blocked_at DESC LIMIT ? OFFSET ?`,
		now, now, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var kind, key, masked, reason, source, blocked, expires, last string
		var attempts int
		if err := rows.Scan(&kind, &key, &masked, &reason, &source, &blocked,
			&expires, &attempts, &last); err != nil {
			return nil, 0, err
		}
		items = append(items, map[string]any{"kind": kind, "key": key,
			"masked_ip": maskBlockKey(masked), "reason": reason, "source": source,
			"blocked_at": s.displayTime(blocked), "expires_at": s.displayTime(expires),
			"attempts_after_block": attempts, "last_attempt_at": s.displayTime(last)})
	}
	return items, total, rows.Err()
}

func (s *Server) createBlock(r *http.Request, kind, key, reason, duration string) error {
	key = strings.TrimSpace(key)
	reason = strings.TrimSpace(reason)
	if key == "" || reason == "" {
		return errors.New("封禁来源和原因不能为空")
	}
	if duration == "" {
		duration = "168h"
	}
	d, err := time.ParseDuration(duration)
	if err != nil || d <= 0 {
		return errors.New("封禁时长必须是有效 duration，例如 168h")
	}
	now := nowText()
	expires := time.Now().UTC().Add(d).Format(time.RFC3339Nano)
	switch kind {
	case "admin":
		_, err = s.repo.DB.ExecContext(r.Context(), `INSERT INTO admin_ip_blocks
			(ip_key, masked_ip, reason, blocked_at, expires_at,
			attempts_after_block, last_attempt_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?)
			ON CONFLICT(ip_key) DO UPDATE SET reason = excluded.reason,
			expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
			key, maskBlockKey(key), reason, now, expires, now, now)
	case "client":
		_, err = s.repo.DB.ExecContext(r.Context(), `INSERT INTO client_blocks
			(client_prefix_key, reason, source, blocked_at, expires_at,
			attempts_after_block, last_attempt_at, updated_at)
			VALUES (?, ?, 'manual', ?, ?, 0, ?, ?)
			ON CONFLICT(client_prefix_key) DO UPDATE SET reason = excluded.reason,
			source = excluded.source, expires_at = excluded.expires_at,
			updated_at = excluded.updated_at`, key, reason, now, expires, now, now)
	default:
		return errors.New("封禁类型必须是 admin 或 client")
	}
	return err
}

func (s *Server) deleteBlock(r *http.Request, kind, key string) error {
	if key == "" {
		return sql.ErrNoRows
	}
	query := map[string]string{
		"admin":  `DELETE FROM admin_ip_blocks WHERE ip_key = ?`,
		"client": `DELETE FROM client_blocks WHERE client_prefix_key = ?`,
	}[kind]
	if query == "" {
		return sql.ErrNoRows
	}
	result, err := s.repo.DB.ExecContext(r.Context(), query, key)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func maskBlockKey(value string) string {
	if len(value) <= 10 {
		return value
	}
	return strings.TrimSpace(value[:10]) + "*"
}
