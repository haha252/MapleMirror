package mirrorsync

import (
	"context"
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
