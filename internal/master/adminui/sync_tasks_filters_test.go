package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncTasksFiltersAcrossNodes(t *testing.T) {
	server, db := newTestServer(t)
	for _, node := range []struct{ id, name string }{{"n1", "香港节点"}, {"n2", "东京节点"}} {
		mustExecAdminUI(t, db, `INSERT INTO nodes (id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at) VALUES (?, ?, 'online', 0, 1, 'now', 'now')`, node.id, node.name)
	}
	for _, task := range []struct{ id, node, state string }{{"t1", "n1", "failed"}, {"t2", "n2", "running"}, {"t3", "n2", "succeeded"}} {
		mustExecAdminUI(t, db, `INSERT INTO node_tasks (id, node_id, task_type, state, request_id, created_at, attempts) VALUES (?, ?, 'inventory_report', ?, 'req', 'now', 1)`, task.id, task.node, task.state)
	}
	for _, tc := range []struct {
		name, query        string
		total, count, code int
	}{
		{"all nodes paginated", "page_size=1", 3, 1, 200},
		{"unfinished across nodes", "active=1", 2, 2, 200},
		{"node scope unchanged", "node_id=n2&active=1", 1, 1, 200},
		{"state", "state=succeeded", 1, 1, 200},
		{"name search", "q=东京", 2, 2, 200},
		{"combined search", "q=东京&active=1", 1, 1, 200},
		{"literal SQL metacharacters", "q=%27%20OR%201%3D1%20--", 0, 0, 200},
		{"unknown state", "state=anything", 0, 0, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			server.syncTasksAPI(rec, httptest.NewRequest(http.MethodGet, "/admin/api/sync/tasks?"+tc.query, nil))
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.code != 200 {
				return
			}
			var data struct {
				Tasks      []map[string]any `json:"tasks"`
				Pagination struct {
					Total int `json:"total"`
				} `json:"pagination"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if data.Pagination.Total != tc.total || len(data.Tasks) != tc.count {
				t.Fatalf("total=%d tasks=%d", data.Pagination.Total, len(data.Tasks))
			}
		})
	}
}
