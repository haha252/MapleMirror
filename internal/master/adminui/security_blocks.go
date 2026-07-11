package adminui

import (
	"database/sql"
	"errors"
	"net"
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
	memoryBlocks := []memoryBlockItem{}
	if s.store.memory != nil {
		memoryBlocks = s.store.memory.activeBlocks(now)
		total += len(memoryBlocks)
	}
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT kind, block_key,
		display_ip, masked_ip, reason, source, blocked_at, expires_at, attempts_after_block,
		escalation_level, punishment_active, last_attempt_at FROM (
		SELECT 'admin' AS kind, ip_key AS block_key,
		COALESCE(NULLIF(display_ip, ''), masked_ip) AS display_ip, masked_ip, reason,
		'管理登录' AS source, blocked_at, expires_at, attempts_after_block,
		0 AS escalation_level, 0 AS punishment_active, last_attempt_at FROM admin_ip_blocks WHERE expires_at > ?
		UNION ALL
		SELECT 'client' AS kind, client_prefix_key AS block_key,
		client_prefix_key AS display_ip, client_prefix_key AS masked_ip, reason, source, blocked_at, expires_at,
		attempts_after_block, escalation_level, punishment_active, last_attempt_at FROM client_blocks WHERE expires_at > ?
		) ORDER BY blocked_at DESC LIMIT ? OFFSET ?`,
		now, now, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var kind, key, display, masked, reason, source, blocked, expires, last string
		var attempts int
		var escalation int
		var punishment int
		if err := rows.Scan(&kind, &key, &display, &masked, &reason, &source, &blocked,
			&expires, &attempts, &escalation, &punishment, &last); err != nil {
			return nil, 0, err
		}
		items = append(items, map[string]any{"kind": kind, "key": key,
			"display_ip": display, "masked_ip": masked, "reason": reason, "source": source,
			"blocked_at": s.displayTime(blocked), "expires_at": s.displayTime(expires),
			"attempts_after_block": attempts, "escalation_level": escalation,
			"punishment_active": punishment == 1, "last_attempt_at": s.displayTime(last)})
	}
	if page.Page == 1 {
		for _, block := range memoryBlocks {
			if len(items) >= page.PageSize {
				break
			}
			items = append(items, map[string]any{"kind": "admin", "key": block.Key,
				"display_ip": block.DisplayIP, "masked_ip": block.MaskedIP,
				"reason": "admin_login_failed", "source": "管理登录",
				"blocked_at":           s.displayTime(block.BlockedAt),
				"expires_at":           s.displayTime(block.ExpiresAt),
				"attempts_after_block": block.AttemptsAfterBlock,
				"escalation_level":     0,
				"punishment_active":    false,
				"last_attempt_at":      s.displayTime(block.LastAttemptAt)})
		}
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
		ip, err := normalizeAdminBlockIP(key)
		if err != nil {
			return err
		}
		key = s.store.ipKey(ip)
		_, err = s.repo.DB.ExecContext(r.Context(), `INSERT INTO admin_ip_blocks
			(ip_key, masked_ip, display_ip, reason, blocked_at, expires_at,
			attempts_after_block, last_attempt_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)
			ON CONFLICT(ip_key) DO UPDATE SET reason = excluded.reason,
			masked_ip = excluded.masked_ip, display_ip = excluded.display_ip,
			expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
			key, maskIP(ip), ip, reason, now, expires, now, now)
	case "client":
		key, err = normalizeClientBlockPrefix(key)
		if err != nil {
			return err
		}
		_, err = s.repo.DB.ExecContext(r.Context(), `INSERT INTO client_blocks
			(client_prefix_key, reason, source, blocked_at, expires_at,
			attempts_after_block, escalation_level, punishment_active,
			last_attempt_at, updated_at)
			VALUES (?, ?, 'manual', ?, ?, 0, 0, 0, ?, ?)
			ON CONFLICT(client_prefix_key) DO UPDATE SET reason = excluded.reason,
			source = excluded.source, blocked_at = excluded.blocked_at,
			expires_at = excluded.expires_at, attempts_after_block = 0,
			escalation_level = 0, punishment_active = 0,
			last_attempt_at = excluded.last_attempt_at, updated_at = excluded.updated_at`, key, reason, now, expires, now, now)
	default:
		return errors.New("封禁类型必须是 admin 或 client")
	}
	if err == nil && kind == "client" && s.resetClientBlockCache != nil {
		s.resetClientBlockCache(key)
	}
	return err
}

func normalizeAdminBlockIP(value string) (string, error) {
	if strings.Contains(value, "/") {
		return "", errors.New("管理登录来源封禁只支持单个 IP，请输入例如 192.0.2.10")
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return "", errors.New("管理登录来源封禁必须是合法 IP")
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String(), nil
	}
	return ip.String(), nil
}

func normalizeClientBlockPrefix(value string) (string, error) {
	if strings.Contains(value, "/") {
		ip, network, err := net.ParseCIDR(value)
		if err != nil {
			return "", errors.New("公开下载客户端封禁必须是合法 IP 或主机前缀")
		}
		ones, bits := network.Mask.Size()
		if bits == 32 && (ones == 24 || ones == 32) {
			return network.String(), nil
		}
		if bits == 128 && ones == 128 && network.IP.Equal(ip) {
			return network.String(), nil
		}
		return "", errors.New("公开下载客户端封禁只支持单 IP、IPv4 /24 网段或 /32、/128 主机前缀")
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return "", errors.New("公开下载客户端封禁必须是合法 IP 或主机前缀")
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}

func (s *Server) deleteBlock(r *http.Request, kind, key string) error {
	if key == "" {
		return sql.ErrNoRows
	}
	switch kind {
	case "admin":
		return s.deleteAdminBlock(r, key)
	case "client":
		return s.deleteClientBlock(r, key)
	default:
		return sql.ErrNoRows
	}
}

func (s *Server) deleteAdminBlock(r *http.Request, key string) error {
	result, err := s.repo.DB.ExecContext(r.Context(),
		`DELETE FROM admin_ip_blocks WHERE ip_key = ?`, key)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Server) deleteClientBlock(r *http.Request, key string) error {
	scope, err := clientLimitScope(key)
	if err != nil {
		return err
	}
	tx, err := s.repo.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(),
		`DELETE FROM client_blocks WHERE client_prefix_key = ?`, key)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if err := s.clearClientLimitScope(r, tx, scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.resetClientBlockCache != nil {
		s.resetClientBlockCache(key)
	}
	if s.resetResourceLimiter != nil {
		s.resetResourceLimiter(key)
	}
	return nil
}
