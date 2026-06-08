package mirrorsync

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestServiceReloadsProjectsBeforeTrigger(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := filepath.Join(t.TempDir(), "projects.yaml")
	writeProjectConfig(t, path, "p1", "owner/one")
	initial, err := config.LoadProjects(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Scanner:  Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}},
		Projects: NewProjectLoader(path, initial),
	}
	if _, err := service.Trigger(context.Background(), "", "req-1"); err != nil {
		t.Fatal(err)
	}
	writeProjectConfig(t, path, "p2", "owner/two")
	if _, err := service.Trigger(context.Background(), "", "req-2"); err != nil {
		t.Fatal(err)
	}
	assertCount(t, db, "projects", 2)
}

func TestTriggerAllCreatesPerProjectScanRows(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := config.Projects{Projects: []config.Project{
		testProject("p1", "owner/one", true),
		testProject("p2", "owner/two", true),
	}}
	service := Service{
		Scanner:  Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}},
		Projects: NewProjectLoader("", projects),
		Interval: time.Minute,
	}
	if _, err := service.Trigger(context.Background(), "", "req-all"); err != nil {
		t.Fatal(err)
	}
	assertScanRows(t, db, "p1", 1)
	assertScanRows(t, db, "p2", 1)
	assertProjectNextScan(t, db, "p1")
	assertProjectNextScan(t, db, "p2")
}

func TestTriggerAllSyncsEmptyProjectConfig(t *testing.T) {
	db, store := seedProjectConfigTask(t)
	defer db.Close()
	service := Service{
		Scanner:  Scanner{Store: store, GitHub: fakeGitHub{releases: testReleases()}},
		Projects: NewProjectLoader("", config.Projects{}),
	}

	scanID, err := service.Trigger(context.Background(), "", "req-empty")
	if err != nil {
		t.Fatal(err)
	}
	if scanID != "" {
		t.Fatalf("empty project list should not create scan, got %q", scanID)
	}
	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
	assertWhereCount(t, db, "node_tasks", "state = 'obsolete'", 4)
}

func TestTriggerAllSyncsDisabledProjectConfig(t *testing.T) {
	db, store := seedProjectConfigTask(t)
	defer db.Close()
	projects := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", false)}}
	service := Service{
		Scanner:  Scanner{Store: store, GitHub: fakeGitHub{releases: testReleases()}},
		Projects: NewProjectLoader("", projects),
	}

	scanID, err := service.Trigger(context.Background(), "", "req-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if scanID != "" {
		t.Fatalf("disabled project should not create scan, got %q", scanID)
	}
	assertProjectEnabled(t, db, "p1", false)
	assertTargetState(t, db, "remove")
	assertWhereCount(t, db, "node_tasks", "state = 'obsolete'", 4)
}

func TestSyncProjectConfigMarksChangedProjectDue(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := Store{DB: db}
	initial := config.Projects{Projects: []config.Project{testProject("p1", "owner/one", true)}}
	if err := store.SyncProjectConfig(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	due, err := store.DueProjects(context.Background(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || len(due) != 1 || due[0] != "p1" {
		t.Fatalf("new project should be due, due=%v err=%v", due, err)
	}
	if err := store.SetProjectNextScan(context.Background(), "p1",
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	unchanged := config.Projects{Projects: []config.Project{testProject("p1", "owner/one", true)}}
	if err := store.SyncProjectConfig(context.Background(), unchanged); err != nil {
		t.Fatal(err)
	}
	due, err = store.DueProjects(context.Background(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || len(due) != 0 {
		t.Fatalf("unchanged project should not be due, due=%v err=%v", due, err)
	}
	changed := config.Projects{Projects: []config.Project{testProject("p1", "owner/two", true)}}
	if err := store.SyncProjectConfig(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	due, err = store.DueProjects(context.Background(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil || len(due) != 1 || due[0] != "p1" {
		t.Fatalf("changed project should be due, due=%v err=%v", due, err)
	}
}

func writeProjectConfig(t *testing.T, path, id, repo string) {
	t.Helper()
	body := `projects:
  - id: "` + id + `"
    name: "` + id + `"
    repository: "` + repo + `"
    enabled: true
    retain_versions: 1
    architecture_regex: "(amd64)"
`
	if err := osWriteFile(path, body); err != nil {
		t.Fatal(err)
	}
}

func osWriteFile(path, body string) error {
	return os.WriteFile(path, []byte(strings.ReplaceAll(body, "\n", "\r\n")), 0o600)
}

func testReleases() []GitHubRelease {
	return []GitHubRelease{{
		ID: 1, TagName: "v1", PublishedAt: time.Now(),
		Assets: []GitHubAsset{{
			ID: 1, Name: "app-amd64.zip", Size: 10,
			URL:    "https://example.invalid/a",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}}
}

func testProject(id, repo string, enabled bool) config.Project {
	return config.Project{
		ID: id, Name: id, Repository: repo, Enabled: enabled,
		RetainVersions: 1, ArchitectureRegex: "(amd64)",
	}
}

func assertScanRows(t *testing.T, db interface {
	QueryRow(string, ...any) *sql.Row
}, projectID string, want int) {
	t.Helper()
	var got int
	err := db.QueryRow(`SELECT COUNT(*) FROM sync_scans WHERE project_id = ?`, projectID).Scan(&got)
	if err != nil || got != want {
		t.Fatalf("scan rows for %s got=%d want=%d err=%v", projectID, got, want, err)
	}
}

func assertProjectNextScan(t *testing.T, db interface {
	QueryRow(string, ...any) *sql.Row
}, projectID string) {
	t.Helper()
	var next string
	err := db.QueryRow(`SELECT COALESCE(next_scan_at, '') FROM project_scan_state
		WHERE project_id = ?`, projectID).Scan(&next)
	if err != nil || next == "" {
		t.Fatalf("next scan missing for %s next=%q err=%v", projectID, next, err)
	}
}
