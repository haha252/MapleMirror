package adminui

import (
	"database/sql"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *Server) downloadHistoryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	displayIP, prefix, ok := normalizeHistoryIP(r.URL.Query().Get("ip"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "请输入合法的单个 IPv4 或 IPv6 地址"})
		return
	}
	retention := s.downloadHistoryRetentionDays
	if retention <= 0 {
		retention = 7
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retention).Format(time.RFC3339Nano)
	page := paginationFrom(r, 20)
	summary, err := s.downloadHistorySummary(r, prefix, cutoff)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "下载历史查询失败"})
		return
	}
	items, total, err := s.downloadHistoryRows(r, prefix, cutoff, page)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "下载历史查询失败"})
		return
	}
	page.Total = total
	writeJSON(w, http.StatusOK, map[string]any{
		"ip": displayIP, "retention_days": retention, "summary": summary,
		"downloads": items, "pagination": page,
	})
}

func normalizeHistoryIP(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "/") {
		return "", "", false
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return "", "", false
	}
	if v4 := ip.To4(); v4 != nil {
		host := v4.String()
		return host, host + "/32", true
	}
	host := ip.String()
	return host, host + "/128", true
}

func (s *Server) downloadHistorySummary(r *http.Request, prefix, cutoff string) (map[string]any, error) {
	var tokens, transfers int
	var sent int64
	err := s.repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN sent_bytes > 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(sent_bytes), 0)
		FROM download_history WHERE client_prefix_key = ? AND issued_at >= ?`,
		prefix, cutoff).Scan(&tokens, &transfers, &sent)
	return map[string]any{"token_count": tokens, "transfer_count": transfers, "sent_bytes": sent}, err
}

func (s *Server) downloadHistoryRows(r *http.Request, prefix, cutoff string,
	page pagination) ([]map[string]any, int, error) {
	var total int
	if err := s.repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM download_history
		WHERE client_prefix_key = ? AND issued_at >= ?`, prefix, cutoff).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT authorization_id, source_kind,
		project_id, project_name, asset_id, file_name, version, system, architecture,
		node_id, node_name, issued_at, expires_at, first_transfer_at, last_transfer_at,
		sent_bytes, status, status_reason, request_id
		FROM download_history WHERE client_prefix_key = ? AND issued_at >= ?
		ORDER BY issued_at DESC, authorization_id DESC LIMIT ? OFFSET ?`,
		prefix, cutoff, page.PageSize, page.offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0, page.PageSize)
	for rows.Next() {
		item, err := s.scanDownloadHistory(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Server) scanDownloadHistory(row rowScanner) (map[string]any, error) {
	var authID, source, projectID, projectName, assetID, fileName, version string
	var system, arch, nodeID, nodeName, issued, expires, first, last string
	var status, reason, requestID string
	var sent int64
	err := row.Scan(&authID, &source, &projectID, &projectName, &assetID, &fileName,
		&version, &system, &arch, &nodeID, &nodeName, &issued, &expires, &first, &last,
		&sent, &status, &reason, &requestID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"authorization_id": authID, "source_kind": source, "project_id": projectID,
		"project_name": projectName, "asset_id": assetID, "file_name": fileName,
		"version": version, "system": system, "architecture": arch, "node_id": nodeID,
		"node_name": nodeName, "issued_at": s.displayTime(issued),
		"expires_at": s.displayTime(expires), "first_transfer_at": s.displayTime(first),
		"last_transfer_at": s.displayTime(last), "sent_bytes": sent, "status": status,
		"status_reason": reason, "request_id": requestID,
	}, nil
}

var _ rowScanner = (*sql.Rows)(nil)
