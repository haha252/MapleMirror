package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNodesInspectionProvidesTargetProgressAndPreciseHeartbeat(t *testing.T) {
	server, db := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, last_heartbeat_at, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, ?, ?, ?)`, now, now, now)
	server.nodeHeartbeatTimeout = 2 * time.Minute
	rec := httptest.NewRecorder()
	server.nodesAPI(rec, httptest.NewRequest(http.MethodGet, "/admin/api/nodes?inspection=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Nodes []struct {
			DownloadReady bool           `json:"download_ready"`
			Heartbeat     int64          `json:"last_heartbeat_unix_ms"`
			Sync          map[string]any `json:"sync"`
		} `json:"nodes"`
		ReportExpiry int64 `json:"report_stale_after_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Nodes) != 1 || body.Nodes[0].DownloadReady || body.Nodes[0].Heartbeat != timestampMillis(now) || body.ReportExpiry != 120000 {
		t.Fatalf("unexpected inspection: %+v", body)
	}
	if body.Nodes[0].Sync["verified_required_assets"] != float64(0) || body.Nodes[0].Sync["sync_phase"] == nil {
		t.Fatalf("missing diagnosis: %+v", body.Nodes[0].Sync)
	}
}

func TestActiveSyncTasksExcludeCompletedHistoryAndPrioritizeFailures(t *testing.T) {
	server, db := newTestServer(t)
	seedNodeProjectAdminData(t, db, 1)
	for _, state := range []string{"running", "failed", "retry_wait", "succeeded", "cancelled"} {
		mustExecAdminUI(t, db, `INSERT INTO node_tasks
			(id, node_id, task_type, state, request_id, created_at, updated_at)
			VALUES (?, 'node-1', 'inventory_reconcile', ?, 'req', 'now', 'now')`, state, state)
	}
	for _, active := range []bool{true, false} {
		url := "/admin/api/sync/tasks?node_id=node-1"
		if active {
			url += "&active=1"
		}
		rec := httptest.NewRecorder()
		server.syncTasksAPI(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var body struct {
			Tasks      []map[string]any `json:"tasks"`
			Pagination pagination       `json:"pagination"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("response=%s", rec.Body.String())
		}
		want := 5
		if active {
			want = 3
			if body.Tasks[0]["state"] != "failed" || body.Tasks[1]["state"] != "retry_wait" {
				t.Fatalf("unexpected active ordering: %+v", body.Tasks)
			}
		}
		if len(body.Tasks) != want || body.Pagination.Total != want {
			t.Fatalf("count=%d total=%d want=%d", len(body.Tasks), body.Pagination.Total, want)
		}
	}
}
