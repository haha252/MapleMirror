package adminui

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mastercontrol "mirror-server/internal/master/control"
)

func TestNodeDeleteAPIUsesHighRiskAndRemovesNode(t *testing.T) {
	server, db := newTestServer(t)
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready, created_at, updated_at)
		VALUES ('node-delete', '待删除节点', 'offline', 0, 0, 'now', 'now')`)

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/nodes/node-delete", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.nodeActionAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertCountAdminUI(t, db, "nodes", 0)
}

func TestNodeProjectsAPIReadsAssignments(t *testing.T) {
	server, db := newTestServer(t)
	seedNodeProjectAdminData(t, db, 1)
	mustExecAdminUI(t, db, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES ('node-1', 'p1', 'manual', 1, 4, 0, '2026-06-07T00:00:00Z', 'now')`)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/nodes/node-1/projects", nil)
	rec := httptest.NewRecorder()
	server.nodeActionAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Projects) != 2 {
		t.Fatalf("projects=%+v", body.Projects)
	}
}

func TestNodeProjectsAPIRejectsManualOverLimit(t *testing.T) {
	server, db := newTestServer(t)
	seedNodeProjectAdminData(t, db, 1)
	req := httptest.NewRequest(http.MethodPut, "/admin/api/nodes/node-1/projects",
		strings.NewReader(`{"assignment_mode":"manual","projects":["p1","p2"]}`))
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.nodeActionAPI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNodeProjectsAPINotifiesRuntimeAfterSave(t *testing.T) {
	server, db := newTestServer(t)
	seedNodeProjectAdminData(t, db, 2)
	runtime := mastercontrol.NewRuntimeStore()
	runtime.StartSession(mastercontrol.Session{ID: "sess-1", NodeID: "node-1"})
	server.syncStore.Runtime = runtime

	req := httptest.NewRequest(http.MethodPut, "/admin/api/nodes/node-1/projects",
		strings.NewReader(`{"assignment_mode":"manual","projects":["p1"]}`))
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.nodeActionAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !runtime.ConsumeSyncTaskWake("node-1") {
		t.Fatal("saving node projects should notify active node")
	}
}

func seedNodeProjectAdminData(t *testing.T, db *sql.DB, max int) {
	t.Helper()
	mustExecAdminUI(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, routing_ready,
		max_mirror_projects, created_at, updated_at)
		VALUES ('node-1', '节点一', 'online', 0, 0, ?, 'now', 'now')`, max)
	for _, id := range []string{"p1", "p2"} {
		mustExecAdminUI(t, db, `INSERT INTO projects
			(id, name, repository, enabled, retain_versions, include_prerelease,
			download_multiplier, config_hash, updated_at)
			VALUES (?, ?, ?, 1, 1, 0, 1, 'hash', 'now')`, id, "项目"+id, "owner/"+id)
	}
}
