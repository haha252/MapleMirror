package adminui

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestProjectVersionResetCreatesDedicatedScan(t *testing.T) {
	server, _ := newTestServer(t)
	versionReset := make(chan string, 1)
	sync := &projectResetSync{versionReset: versionReset}
	server.sync = sync

	req := httptest.NewRequest(http.MethodPost, "/admin/api/projects/p1/reset-versions", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.projectActionAPI(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("version reset status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case projectID := <-versionReset:
		if projectID != "p1" {
			t.Fatalf("version reset project = %q, want p1", projectID)
		}
	case <-time.After(time.Second):
		t.Fatal("version reset scan was not triggered")
	}
	if sync.called {
		t.Fatal("version reset must not use the ordinary scan trigger")
	}
}

func TestScanAPIDetachesTriggerFromRequestContext(t *testing.T) {
	server, _ := newTestServer(t)
	sync := &asyncScanSync{
		started: make(chan struct{}),
		check:   make(chan struct{}),
		result:  make(chan error, 1),
	}
	server.sync = sync
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/admin/api/sync/scans",
		strings.NewReader(`{"project_id":"p1"}`))
	req.RemoteAddr = "127.0.0.1:55000"
	req = withAdminUser(req)
	rec := httptest.NewRecorder()

	server.scanAPI(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("scan status = %d body=%s", rec.Code, rec.Body.String())
	}
	waitForAsyncScan(t, sync.started)
	cancel()
	close(sync.check)
	if err := waitForAsyncScanResult(t, sync.result); err != nil {
		t.Fatalf("background scan context should be detached, got %v", err)
	}
}

type projectResetSync struct {
	called       bool
	versionReset chan string
}

func (s *projectResetSync) Trigger(context.Context, string, string) (string, error) {
	s.called = true
	return "scan-new", nil
}

func (s *projectResetSync) TriggerVersionReset(_ context.Context, projectID, _ string) (string, error) {
	if s.versionReset != nil {
		s.versionReset <- projectID
	}
	return "scan-version-reset", nil
}

type asyncScanSync struct {
	started chan struct{}
	check   chan struct{}
	result  chan error
}

func (s *asyncScanSync) Trigger(ctx context.Context, _, _ string) (string, error) {
	close(s.started)
	<-s.check
	s.result <- ctx.Err()
	return "scan-new", nil
}

func (s *asyncScanSync) TriggerVersionReset(context.Context, string, string) (string, error) {
	return "scan-version-reset", nil
}

func waitForAsyncScan(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("background scan did not start")
	}
}

func waitForAsyncScanResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("background scan did not finish")
		return nil
	}
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
