package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNodeDeleteAPIUsesHighRiskAndRemovesNode(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-delete', '待删除节点', 'offline', 0, 0, 'now', 'now')`)

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/nodes/node-delete", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	rec := httptest.NewRecorder()
	server.nodeActionAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertCountAdminUI(t, db, "nodes", 0)
}
