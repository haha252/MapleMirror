package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditEventsHaveDistinctStableIdentities(t *testing.T) {
	server, db := newTestServer(t)
	// One request may write several events within the same displayed second.
	for _, id := range []string{"event-a", "event-b"} {
		mustExecAdminUI(t, db, `INSERT INTO admin_audit_events (id, operation, target_type, target_id, result, request_id, created_at) VALUES (?, 'config.save', 'project', 'p1', 'success', 'req', '2026-10-04T00:00:00Z')`, id)
	}
	var ids []string
	for _, path := range []string{"/admin/api/security/audit-events?page=1&page_size=1", "/admin/api/security/audit-events?page=2&page_size=1"} {
		rec := httptest.NewRecorder()
		server.auditEventsAPI(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var data struct {
			Events []struct {
				ID string `json:"id"`
			} `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Events) != 1 || data.Events[0].ID == "" {
			t.Fatalf("events=%+v", data.Events)
		}
		ids = append(ids, data.Events[0].ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("pagination repeated event %s", ids[0])
	}
}
