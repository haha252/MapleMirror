package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func TestScanAPIReturnsProjectScanState(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO project_scan_state
		(project_id, enabled, config_hash, last_scan_started_at,
		last_scan_completed_at, last_scan_id, last_scan_state,
		next_scan_at, updated_at)
		VALUES ('p1', 1, 'hash', 'start', 'done', 'scan-1',
		'succeeded', 'next', 'done')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO sync_scans
		(id, project_id, state, request_id, started_at, completed_at)
		VALUES ('scan-1', 'p1', 'succeeded', 'req-1', 'start', 'done')`)
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Auth: testAuth(t), SyncStore: mirrorsync.Store{DB: db}}
	req := adminRequest("GET", "/api/admin/v1/sync/scans", "")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody response
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	raw, ok := listBody.Data.(map[string]any)["projects"].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("unexpected list data: %#v", listBody.Data)
	}

	req = adminRequest("GET", "/api/admin/v1/sync/scans/latest?project_id=p1", "")
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("latest status=%d body=%s", rec.Code, rec.Body.String())
	}
	var latestBody response
	if err := json.Unmarshal(rec.Body.Bytes(), &latestBody); err != nil {
		t.Fatal(err)
	}
	data := latestBody.Data.(map[string]any)
	if data["next_scan_at"] != "next" {
		t.Fatalf("next_scan_at missing: %#v", data)
	}
}

func testAuth(t *testing.T) Auth {
	t.Helper()
	t.Setenv("MIRROR_TEST_ADMIN_TOKEN", "abcdefghijklmnopqrstuvwxyz123456")
	auth, err := NewAuth(config.Administration{
		AllowedCIDRs:  []string{"127.0.0.0/8"},
		TokenEnv:      "MIRROR_TEST_ADMIN_TOKEN",
		TokenMinBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func adminRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz123456")
	return req
}
