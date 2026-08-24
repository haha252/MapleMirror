package mirrorsync

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/indexnow"
	"mirror-server/internal/storage"
)

func TestPublicPageChangesIncludesOnlyChangedAddedAndDeletedPages(t *testing.T) {
	before := indexnow.Snapshot{
		"/":        {Path: "/", Fingerprint: "home-old", Present: true},
		"/about":   {Path: "/about", Fingerprint: "about", Present: true},
		"/old/":    {Path: "/old/", Fingerprint: "old", Present: true},
		"/same/":   {Path: "/same/", Fingerprint: "same", Present: true},
		"/update/": {Path: "/update/", Fingerprint: "old", Present: true},
	}
	after := indexnow.Snapshot{
		"/":        {Path: "/", Fingerprint: "home-new", Present: true},
		"/about":   {Path: "/about", Fingerprint: "about", Present: true},
		"/new/":    {Path: "/new/", Fingerprint: "new", Present: true},
		"/same/":   {Path: "/same/", Fingerprint: "same", Present: true},
		"/update/": {Path: "/update/", Fingerprint: "new", Present: true},
	}
	changes := publicPageChanges(before, after)
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Path)
	}
	want := []string{"/", "/new/", "/old/", "/update/"}
	if len(got) != len(want) {
		t.Fatalf("changed pages=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changed pages=%v want %v", got, want)
		}
	}
	if changes[2].Present {
		t.Fatal("deleted page should be marked absent")
	}
}

func TestPublicSnapshotContainsStaticPagesAndEnabledProjects(t *testing.T) {
	db := openPublicTestDB(t)
	defer db.Close()
	scanner := Scanner{Store: Store{DB: db}, SEORevision: "seo-test"}
	projects := config.Projects{Projects: []config.Project{
		testProject("p1", "owner/repo", true),
		testProject("disabled", "owner/disabled", false),
	}}
	snapshot, err := scanner.PublicSnapshot(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/about", "/api-docs", "/stats", "/changelog", "/p1/"} {
		if page, ok := snapshot[path]; !ok || !page.Present || page.Fingerprint == "" {
			t.Fatalf("snapshot missing public page %q: %+v", path, snapshot)
		}
	}
	if _, ok := snapshot["/disabled/"]; ok {
		t.Fatalf("disabled project should not be public: %+v", snapshot)
	}
}

func TestPublicSEORevisionChangesOnlyStaticPages(t *testing.T) {
	db := openPublicTestDB(t)
	defer db.Close()
	projects := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", true)}}
	oldSnapshot, err := (Scanner{Store: Store{DB: db}, SEORevision: "seo-old"}).PublicSnapshot(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	newSnapshot, err := (Scanner{Store: Store{DB: db}, SEORevision: "seo-new"}).PublicSnapshot(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	changes := publicPageChanges(oldSnapshot, newSnapshot)
	want := []string{"/", "/about", "/api-docs", "/changelog", "/stats"}
	if len(changes) != len(want) {
		t.Fatalf("SEO revision changes=%v want %v", changes, want)
	}
	for i, path := range want {
		if changes[i].Path != path || !changes[i].Present {
			t.Fatalf("SEO revision changes=%v want present paths %v", changes, want)
		}
	}
}

type publicNotifierRecorder struct {
	changes   [][]indexnow.PageChange
	snapshots []indexnow.Snapshot
}

func (r *publicNotifierRecorder) NotifyChanges(_ context.Context, changes []indexnow.PageChange) {
	r.changes = append(r.changes, append([]indexnow.PageChange(nil), changes...))
}

func (r *publicNotifierRecorder) NotifySnapshotNow(_ context.Context, snapshot indexnow.Snapshot) int {
	r.snapshots = append(r.snapshots, snapshot)
	return len(snapshot)
}

func TestServiceTriggerFullPublicNotificationQueuesCurrentPages(t *testing.T) {
	db := openPublicTestDB(t)
	defer db.Close()
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()},
		Notifier: &publicNotifierRecorder{}, SEORevision: "seo-test"}
	projects := config.Projects{Projects: []config.Project{testProject("p1", "owner/repo", true)}}
	if _, err := scanner.Scan(context.Background(), projects, "", "seed"); err != nil {
		t.Fatal(err)
	}
	recorder := scanner.Notifier.(*publicNotifierRecorder)
	recorder.changes = nil
	queued, err := (Service{Scanner: scanner}).TriggerFullPublicNotification(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if queued != 6 || len(recorder.snapshots) != 1 {
		t.Fatalf("queued=%d snapshots=%d want 6/1", queued, len(recorder.snapshots))
	}
	for _, path := range []string{"/", "/about", "/api-docs", "/changelog", "/p1/", "/stats"} {
		if _, ok := recorder.snapshots[0][path]; !ok {
			t.Fatalf("manual snapshot missing %q: %+v", path, recorder.snapshots[0])
		}
	}
}

func TestScannerNotifiesOnlyChangedPublicPages(t *testing.T) {
	db := openPublicTestDB(t)
	defer db.Close()
	recorder := &publicNotifierRecorder{}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()},
		Notifier: recorder, SEORevision: "seo-test"}
	project := testProject("p1", "owner/repo", true)
	projects := config.Projects{Projects: []config.Project{project}}

	if _, err := scanner.Scan(context.Background(), projects, "", "first"); err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, recorder, []string{"/", "/p1/"})
	recorder.changes = nil
	if _, err := scanner.Scan(context.Background(), projects, "", "same"); err != nil {
		t.Fatal(err)
	}
	if len(recorder.changes) != 0 {
		t.Fatalf("unchanged scan notified=%v", recorder.changes)
	}

	project.Description = "changed"
	projects.Projects[0] = project
	if _, err := scanner.Scan(context.Background(), projects, "", "updated"); err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, recorder, []string{"/", "/p1/"})
	recorder.changes = nil
	if _, err := scanner.Scan(context.Background(), config.Projects{}, "", "removed"); err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, recorder, []string{"/", "/p1/"})
	if recorder.changes[0][1].Present {
		t.Fatalf("removed project should be absent: %+v", recorder.changes)
	}
}

func assertChangePaths(t *testing.T, recorder *publicNotifierRecorder, want []string) {
	t.Helper()
	if len(recorder.changes) != 1 || len(recorder.changes[0]) != len(want) {
		t.Fatalf("notifications=%v want one call %v", recorder.changes, want)
	}
	for i, path := range want {
		if recorder.changes[0][i].Path != path {
			t.Fatalf("notification=%v want %v", recorder.changes[0], want)
		}
	}
}

func openPublicTestDB(t *testing.T) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedNode(t, db)
	return db
}
