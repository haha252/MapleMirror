package adminui

import (
	"net/http"
	"strings"
)

func (s *Server) statsOverviewAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	day := queryDay(r)
	var auth, started, daily, total int64
	_ = s.repo.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(p.authorization_count, 0),
		COALESCE(p.transfer_started_count, 0),
		MAX(COALESCE(p.sent_bytes, 0), COALESCE((
			SELECT SUM(sent_bytes) FROM daily_node_traffic_stats WHERE stat_day = ?
		), 0))
		FROM (SELECT 1) seed
		LEFT JOIN daily_public_stats p ON p.stat_day = ?`, day, day).Scan(&auth, &started, &daily)
	_ = s.repo.DB.QueryRowContext(r.Context(), `SELECT
		MAX(COALESCE(t.sent_bytes, 0), COALESCE((
			SELECT SUM(sent_bytes) FROM node_traffic_totals
		), 0))
		FROM (SELECT 1) seed
		LEFT JOIN public_stat_totals t ON t.id = 'global'`).Scan(&total)
	writeJSON(w, http.StatusOK, map[string]any{"stat_day": day,
		"authorization_count": auth, "transfer_started_count": started,
		"daily_sent_bytes": daily, "total_sent_bytes": total})
}

func (s *Server) projectStatsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	day := queryDay(r)
	rows, err := s.repo.DB.QueryContext(r.Context(), `SELECT project_id,
		authorization_count, transfer_started_count, sent_bytes
		FROM daily_project_stats WHERE stat_day = ? ORDER BY project_id`, day)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "项目统计查询失败"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var projectID string
		var auth, started, bytes int64
		if err := rows.Scan(&projectID, &auth, &started, &bytes); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "项目统计读取失败"})
			return
		}
		items = append(items, map[string]any{"project_id": projectID,
			"authorization_count": auth, "transfer_started_count": started,
			"sent_bytes": bytes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"stat_day": day, "projects": items})
}

func (s *Server) authorizationAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/api/authorizations/")
	var assetID, nodeID, prefix, reqID, state, first, expires string
	var sent int64
	err := s.repo.DB.QueryRowContext(r.Context(), `SELECT da.asset_id, da.node_id,
		da.client_prefix_key, da.request_id, da.status, COALESCE(da.first_transfer_at, ''),
		da.expires_at, COALESCE(tr.settled_bytes, 0)
		FROM download_authorizations da LEFT JOIN traffic_reservations tr
		ON tr.authorization_id = da.id WHERE da.id = ?`, id).
		Scan(&assetID, &nodeID, &prefix, &reqID, &state, &first, &expires, &sent)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "授权不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorization_id": id,
		"asset_id": assetID, "node_id": nodeID, "client_prefix": prefix,
		"request_id": reqID, "state": state,
		"first_transfer_at": s.displayTime(first),
		"expires_at":        s.displayTime(expires), "sent_bytes": sent})
}

func queryDay(r *http.Request) string {
	day := r.URL.Query().Get("day")
	if day == "" {
		day = timeNowDay()
	}
	return day
}
