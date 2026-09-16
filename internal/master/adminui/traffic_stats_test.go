package adminui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrafficOverviewMergesGlobalAndNodeLedgers(t *testing.T) {
	server, db := newTestServer(t)
	day := timeNowDay()
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-traffic', '流量节点', 'online', 0, 1, 'now', 'now')`)
	mustExecAdminUI(t, db, `INSERT INTO daily_public_stats
		(stat_day, authorization_count, transfer_started_count, sent_bytes, updated_at)
		VALUES (?, 3, 2, 4096, 'now')`, day)
	mustExecAdminUI(t, db, `UPDATE public_stat_totals SET
		authorization_count = 3, transfer_started_count = 2,
		sent_bytes = 4096, updated_at = 'now' WHERE id = 'global'`)
	mustExecAdminUI(t, db, `INSERT INTO daily_node_traffic_stats
		(stat_day, node_id, sent_bytes, updated_at)
		VALUES (?, 'node-traffic', 8192, 'now')`, day)
	mustExecAdminUI(t, db, `INSERT INTO node_traffic_totals
		(node_id, sent_bytes, updated_at)
		VALUES ('node-traffic', 8192, 'now')`)

	assertAdminTrafficOverview(t, server, day, 8192, 8192)

	mustExecAdminUI(t, db, `UPDATE daily_public_stats SET sent_bytes = 12288 WHERE stat_day = ?`, day)
	mustExecAdminUI(t, db, `UPDATE public_stat_totals SET sent_bytes = 12288 WHERE id = 'global'`)
	assertAdminTrafficOverview(t, server, day, 12288, 12288)

	if err := server.repo.DeleteNode(context.Background(), "node-traffic", "req-delete", "admin"); err != nil {
		t.Fatal(err)
	}
	assertAdminTrafficOverview(t, server, day, 12288, 12288)

	overview, err := server.overviewData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Stats["daily_sent_bytes"] != int64(12288) ||
		overview.Stats["total_sent_bytes"] != int64(12288) {
		t.Fatalf("overview traffic mismatch: %+v", overview.Stats)
	}
}

func assertAdminTrafficOverview(t *testing.T, server *Server, day string, wantDaily, wantTotal int64) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/stats/overview?day="+day, nil)
	rec := httptest.NewRecorder()
	server.statsOverviewAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Daily int64 `json:"daily_sent_bytes"`
		Total int64 `json:"total_sent_bytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Daily != wantDaily || body.Total != wantTotal {
		t.Fatalf("traffic overview mismatch: got daily=%d total=%d want daily=%d total=%d",
			body.Daily, body.Total, wantDaily, wantTotal)
	}
}
