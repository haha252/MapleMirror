package adminui

import (
	"context"
	"database/sql"
	"net"
	"strings"
)

const activeBlockRowsSQL = `SELECT kind, block_key, display_ip, masked_ip, reason, source,
	blocked_at, expires_at, attempts_after_block, escalation_level, punishment_active,
	last_attempt_at FROM (
		SELECT 'admin' AS kind, ip_key AS block_key,
			COALESCE(NULLIF(display_ip, ''), masked_ip) AS display_ip, masked_ip, reason,
			'管理登录' AS source, blocked_at, expires_at, attempts_after_block,
			0 AS escalation_level, 0 AS punishment_active, last_attempt_at
		FROM admin_ip_blocks WHERE expires_at > ?
		UNION ALL
		SELECT 'client' AS kind, client_prefix_key AS block_key,
			client_prefix_key AS display_ip, client_prefix_key AS masked_ip, reason, source,
			blocked_at, expires_at, attempts_after_block, escalation_level,
			punishment_active, last_attempt_at
		FROM client_blocks WHERE expires_at > ?
	)`

func (s *Server) queryStoredBlocks(ctx context.Context, now string, page pagination,
	query string) ([]map[string]any, int, error) {
	filter, filterArgs := blockSearchFilter(query)
	baseArgs := []any{now, now}
	countArgs := append(append([]any{}, baseArgs...), filterArgs...)
	var total int
	if err := s.repo.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (`+activeBlockRowsSQL+`)`+filter, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append(append([]any{}, baseArgs...), filterArgs...)
	args = append(args, page.PageSize, page.offset())
	rows, err := s.repo.DB.QueryContext(ctx, activeBlockRowsSQL+filter+
		` ORDER BY blocked_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0, page.PageSize)
	for rows.Next() {
		item, err := s.scanStoredBlock(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func blockSearchFilter(query string) (string, []any) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}
	like := strings.ToLower(query)
	keys := blockSearchKeys(query)
	filter := ` WHERE (instr(lower(display_ip), ?) > 0 OR instr(lower(masked_ip), ?) > 0`
	args := []any{like, like}
	for range keys {
		filter += ` OR block_key = ?`
	}
	filter += `)`
	for _, key := range keys {
		args = append(args, key)
	}
	return filter, args
}

func blockSearchKeys(query string) []string {
	if strings.Contains(query, "/") {
		if _, network, err := net.ParseCIDR(query); err == nil {
			return []string{network.String()}
		}
		return nil
	}
	ip := net.ParseIP(query)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		host := v4.String()
		_, network, _ := net.ParseCIDR(host + "/24")
		return []string{host + "/32", network.String()}
	}
	return []string{ip.String() + "/128"}
}

func blockMatchesSearch(block memoryBlockItem, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(block.DisplayIP), query) ||
		strings.Contains(strings.ToLower(block.MaskedIP), query)
}

func (s *Server) scanStoredBlock(rows *sql.Rows) (map[string]any, error) {
	var kind, key, display, masked, reason, source, blocked, expires, last string
	var attempts, escalation, punishment int
	if err := rows.Scan(&kind, &key, &display, &masked, &reason, &source, &blocked,
		&expires, &attempts, &escalation, &punishment, &last); err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": kind, "key": key, "display_ip": display, "masked_ip": masked,
		"reason": reason, "source": source, "blocked_at": s.displayTime(blocked),
		"expires_at": s.displayTime(expires), "attempts_after_block": attempts,
		"escalation_level": escalation, "punishment_active": punishment == 1,
		"last_attempt_at": s.displayTime(last),
	}, nil
}
