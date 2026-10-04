package adminui

import (
	"context"
	"net/http"
	"time"

	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/master/public"
)

func (s *Server) nodesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "接口不存在"})
		return
	}
	items, err := s.repo.ListNodes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "节点列表查询失败"})
		return
	}
	rows := s.nodeSummaries(r.Context(), items)
	if r.URL.Query().Get("inspection") == "1" {
		if err := s.addNodeInspection(r.Context(), rows); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "节点巡检状态查询失败"})
			return
		}
	}
	expiry := s.nodeHeartbeatTimeout
	if expiry <= 0 {
		expiry = 90 * time.Second
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": rows,
		"server_now_unix_ms": time.Now().UnixMilli(), "report_stale_after_ms": expiry.Milliseconds()})
}

func (s *Server) addNodeInspection(ctx context.Context, rows []map[string]any) error {
	statuses, err := s.syncStore.SyncStatuses(ctx)
	if err != nil {
		return err
	}
	sync := map[string]mirrorsync.SyncStatus{}
	for _, status := range statuses {
		sync[status.NodeID] = status
	}
	available, err := (public.Store{DB: s.repo.DB, PublicProbeNetworkFailures: s.publicProbeNetworkFailures}).DownloadReadyNodes(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		id, _ := row["node_id"].(string)
		status := sync[id]
		row["sync"] = s.syncStatusResponse(status)
		row["download_ready"] = available[id]
	}
	return nil
}

func timestampMillis(value any) int64 {
	if text, ok := value.(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UnixMilli()
		}
	}
	if parsed, ok := value.(time.Time); ok && !parsed.IsZero() {
		return parsed.UnixMilli()
	}
	return 0
}
