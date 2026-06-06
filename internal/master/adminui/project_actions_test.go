package adminui

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/mirrorsync"
)

func TestProjectResetRejectsDisabledConfiguredProject(t *testing.T) {
	server, db := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecAdminUI(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', 'p1', 'owner/one', 0, 1, 0, 1, 'hash', ?)`, now)
	mustExecAdminUI(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('p1:1', 'p1', 1, 'v1', 0, ?, 1, ?)`, now, now)
	server.projects = mirrorsync.NewProjectLoader("", config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "p1", Repository: "owner/one", Enabled: false, RetainVersions: 1,
	}}})
	sync := &projectResetSync{}
	server.sync = sync

	req := httptest.NewRequest(http.MethodPost, "/admin/api/projects/p1/reset", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.projectActionAPI(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reset status = %d body=%s", rec.Code, rec.Body.String())
	}
	if sync.called {
		t.Fatal("disabled project reset must not trigger a scan")
	}
	assertCountAdminUI(t, db, "releases", 1)
}

type projectResetSync struct {
	called bool
}

func (s *projectResetSync) Trigger(context.Context, string, string) (string, error) {
	s.called = true
	return "scan-new", nil
}

func assertCountAdminUI(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s got=%d want=%d", table, got, want)
	}
}
