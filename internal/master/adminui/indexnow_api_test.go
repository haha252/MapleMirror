package adminui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexNowAPIQueuesFullPublicSubmissionForOwner(t *testing.T) {
	server, db := newTestServer(t)
	t.Cleanup(func() { _ = db.Close() })
	trigger := &indexNowTriggerRecorder{queued: 12}
	server.indexNow = trigger
	req := httptest.NewRequest(http.MethodPost, "/admin/api/indexnow/submit", nil)
	req = withAdminUser(req)
	rec := httptest.NewRecorder()

	server.indexNowAPI(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("IndexNow status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["url_count"] != float64(12) || trigger.calls != 1 {
		t.Fatalf("response=%v trigger_calls=%d", body, trigger.calls)
	}
}

func TestIndexNowAPIRequiresOwner(t *testing.T) {
	server, db := newTestServer(t)
	t.Cleanup(func() { _ = db.Close() })
	server.indexNow = &indexNowTriggerRecorder{queued: 1}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/indexnow/submit", nil)
	req = req.WithContext(context.WithValue(req.Context(), usernameKey{}, "missing"))
	rec := httptest.NewRecorder()

	server.indexNowAPI(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner IndexNow status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIndexNowTriggerIsOnProjectsPage(t *testing.T) {
	server, db := newTestServer(t)
	t.Cleanup(func() { _ = db.Close() })
	for _, path := range []string{"/admin/", "/admin/projects"} {
		req := withAdminUser(httptest.NewRequest(http.MethodGet, path, nil))
		rec := httptest.NewRecorder()
		server.shell(rec, req)
		present := strings.Contains(rec.Body.String(), `id="indexnow-submit"`)
		if present != (path == "/admin/projects") {
			t.Fatalf("IndexNow button placement mismatch on %s", path)
		}
	}
}

type indexNowTriggerRecorder struct {
	calls  int
	queued int
}

func (r *indexNowTriggerRecorder) TriggerFullPublicNotification(context.Context) (int, error) {
	r.calls++
	return r.queued, nil
}
