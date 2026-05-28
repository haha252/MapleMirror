package admin

import (
	"net/http"
	"strings"
	"time"
)

func (s Server) statsOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = timeNowDay()
	}
	var auth, started, daily, total int64
	_ = s.Repo.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(authorization_count), 0),
		COALESCE(SUM(transfer_started_count), 0),
		COALESCE(SUM(sent_bytes), 0) FROM daily_project_stats
		WHERE stat_day = ?`, day).Scan(&auth, &started, &daily)
	_ = s.Repo.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(sent_bytes), 0)
		FROM traffic_events WHERE accounted_at IS NOT NULL`).Scan(&total)
	writeOK(w, r, http.StatusOK, "统计总览", map[string]any{
		"stat_day": day, "authorization_count": auth,
		"transfer_started_count": started, "daily_sent_bytes": daily,
		"total_sent_bytes": total,
	})
}

func (s Server) projectStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = timeNowDay()
	}
	rows, err := s.Repo.DB.QueryContext(r.Context(), `SELECT project_id,
		authorization_count, transfer_started_count, sent_bytes
		FROM daily_project_stats WHERE stat_day = ? ORDER BY project_id`, day)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "项目统计查询失败")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var projectID string
		var auth, started, bytes int64
		if err := rows.Scan(&projectID, &auth, &started, &bytes); err != nil {
			writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "项目统计读取失败")
			return
		}
		items = append(items, map[string]any{"project_id": projectID,
			"authorization_count": auth, "transfer_started_count": started, "sent_bytes": bytes})
	}
	writeOK(w, r, http.StatusOK, "项目统计", map[string]any{"stat_day": day, "projects": items})
}

func (s Server) authorization(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/v1/authorizations/")
	var assetID, nodeID, prefix, reqID, status, first, expires string
	var sent int64
	err := s.Repo.DB.QueryRowContext(r.Context(), `SELECT da.asset_id, da.node_id,
		da.client_prefix_key, da.request_id, da.status, COALESCE(da.first_transfer_at, ''),
		da.expires_at, COALESCE(SUM(te.sent_bytes), 0)
		FROM download_authorizations da LEFT JOIN traffic_events te
		ON te.authorization_id = da.id AND te.accounted_at IS NOT NULL
		WHERE da.id = ? GROUP BY da.id`, id).
		Scan(&assetID, &nodeID, &prefix, &reqID, &status, &first, &expires, &sent)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "RESOURCE_NOT_FOUND", "授权不存在")
		return
	}
	writeOK(w, r, http.StatusOK, "授权状态", map[string]any{
		"authorization_id": id, "asset_id": assetID, "node_id": nodeID,
		"client_prefix": prefix, "request_id": reqID, "state": status,
		"first_transfer_at": first, "expires_at": expires, "sent_bytes": sent,
	})
}

func (s Server) trafficEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	rows, err := s.Repo.DB.QueryContext(r.Context(), `SELECT node_id, event_sequence,
		authorization_id, node_request_id, master_request_id, sent_bytes, status, accounted_at
		FROM traffic_events WHERE authorization_id = ? ORDER BY node_id, event_sequence LIMIT 100`,
		r.URL.Query().Get("authorization_id"))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "流量事件查询失败")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var nodeID, authID, nodeReq, masterReq, status, accounted string
		var seq, bytes int64
		if err := rows.Scan(&nodeID, &seq, &authID, &nodeReq, &masterReq, &bytes, &status, &accounted); err != nil {
			writeError(w, r, http.StatusInternalServerError, "CONTROL_INTERNAL_ERROR", "流量事件读取失败")
			return
		}
		items = append(items, map[string]any{"node_id": nodeID, "event_sequence": seq,
			"authorization_id": authID, "node_request_id": nodeReq,
			"master_request_id": masterReq, "sent_bytes": bytes,
			"status": status, "accounted_at": accounted})
	}
	writeOK(w, r, http.StatusOK, "流量事件", map[string]any{"events": items})
}

func (s Server) nodeSLA(w http.ResponseWriter, r *http.Request, nodeID string) {
	if _, ok := s.require(w, r, false); !ok {
		return
	}
	items := []map[string]any{
		s.slaWindow(r, nodeID, "24h", 24),
		s.slaWindow(r, nodeID, "7d", 24*7),
		s.slaWindow(r, nodeID, "30d", 24*30),
	}
	writeOK(w, r, http.StatusOK, "节点 SLA", map[string]any{"node_id": nodeID, "windows": items})
}

func (s Server) slaWindow(r *http.Request, nodeID, name string, hours int) map[string]any {
	start := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339Nano)
	var total, ok int64
	_ = s.Repo.DB.QueryRowContext(r.Context(), `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN routable = 1 AND heartbeat_ok = 1 THEN 1 ELSE 0 END), 0)
		FROM node_availability_samples WHERE node_id = ? AND sample_start >= ?`,
		nodeID, start).Scan(&total, &ok)
	ratio := 0.0
	if total > 0 {
		ratio = float64(ok) / float64(total)
	}
	return map[string]any{"window": name, "availability_ratio": ratio,
		"sample_count": total, "insufficient_samples": total < 3}
}

func timeNowDay() string {
	return time.Now().In(time.Local).Format("2006-01-02")
}
