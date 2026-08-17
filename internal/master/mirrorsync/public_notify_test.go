package mirrorsync

import (
	"context"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestChangedPublicPathsIncludesChangedEnabledProjectsAndHome(t *testing.T) {
	before := map[string]publicFingerprint{
		"same":     {Enabled: true, Hash: "same"},
		"updated":  {Enabled: true, Hash: "old"},
		"disabled": {Enabled: false, Hash: "old"},
		"removed":  {Enabled: true, Hash: "old"},
	}
	after := map[string]publicFingerprint{
		"same":     {Enabled: true, Hash: "same"},
		"updated":  {Enabled: true, Hash: "new"},
		"disabled": {Enabled: false, Hash: "new"},
		"new":      {Enabled: true, Hash: "new"},
	}
	got := changedPublicPaths(before, after)
	want := []string{"/", "/new/", "/removed/", "/updated/"}
	if len(got) != len(want) {
		t.Fatalf("changed paths=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changed paths=%v want %v", got, want)
		}
	}
}

func TestChangedPublicPathsEscapesProjectIDAndIgnoresDisabledOnlyChanges(t *testing.T) {
	before := map[string]publicFingerprint{
		"disabled": {Enabled: false, Hash: "old"},
	}
	after := map[string]publicFingerprint{
		"disabled":    {Enabled: false, Hash: "new"},
		"new project": {Enabled: true, Hash: "new"},
	}
	got := changedPublicPaths(before, after)
	want := []string{"/", "/new%20project/"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("changed paths=%v want %v", got, want)
	}
}

func TestFullPublicPathsIncludesStaticPagesAndEveryEnabledProject(t *testing.T) {
	before := map[string]publicFingerprint{
		"old":      {Enabled: true, Hash: "old"},
		"disabled": {Enabled: false, Hash: "old"},
	}
	after := map[string]publicFingerprint{
		"old":      {Enabled: true, Hash: "same"},
		"new":      {Enabled: true, Hash: "new"},
		"disabled": {Enabled: false, Hash: "new"},
	}
	got := fullPublicPaths(before, after)
	want := []string{"/", "/about", "/api-docs", "/changelog", "/new/", "/old/", "/stats"}
	if len(got) != len(want) {
		t.Fatalf("full public paths=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("full public paths=%v want %v", got, want)
		}
	}
}

type publicNotifierRecorder struct {
	calls [][]string
}

func (r *publicNotifierRecorder) NotifyPaths(_ context.Context, paths []string) {
	r.calls = append(r.calls, append([]string(nil), paths...))
}

func TestScannerNotifiesAllPublicPagesAfterSuccessfulScan(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	recorder := &publicNotifierRecorder{}
	scanner := Scanner{Store: Store{DB: db}, GitHub: fakeGitHub{releases: testReleases()}, Notifier: recorder}
	project := testProject("p1", "owner/repo", true)
	projects := config.Projects{Projects: []config.Project{project}}

	if _, err := scanner.Scan(context.Background(), projects, "", "first"); err != nil {
		t.Fatal(err)
	}
	fullPaths := []string{"/", "/about", "/api-docs", "/changelog", "/p1/", "/stats"}
	assertPublicNotification(t, recorder, fullPaths)
	recorder.calls = nil
	if _, err := scanner.Scan(context.Background(), projects, "", "same"); err != nil {
		t.Fatal(err)
	}
	assertPublicNotification(t, recorder, fullPaths)
	recorder.calls = nil

	project.Description = "changed"
	projects.Projects[0] = project
	if _, err := scanner.Scan(context.Background(), projects, "", "updated"); err != nil {
		t.Fatal(err)
	}
	assertPublicNotification(t, recorder, fullPaths)
	recorder.calls = nil
	if _, err := scanner.Scan(context.Background(), config.Projects{}, "", "removed"); err != nil {
		t.Fatal(err)
	}
	assertPublicNotification(t, recorder, fullPaths)
}

func assertPublicNotification(t *testing.T, recorder *publicNotifierRecorder, want []string) {
	t.Helper()
	if len(recorder.calls) != 1 || len(recorder.calls[0]) != len(want) {
		t.Fatalf("notifications=%v want one call %v", recorder.calls, want)
	}
	for i := range want {
		if recorder.calls[0][i] != want[i] {
			t.Fatalf("notification=%v want %v", recorder.calls[0], want)
		}
	}
}
