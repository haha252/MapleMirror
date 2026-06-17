package mirrorsync

import (
	"context"
	"path/filepath"
	"testing"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/storage"
)

func TestScanNotifiesRuntimeWhenTasksGenerated(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedNode(t, db)
	runtime := mastercontrol.NewRuntimeStore()
	runtime.StartSession(mastercontrol.Session{ID: "sess-1", NodeID: "node-1"})
	scanner := Scanner{
		Store:  Store{DB: db, Runtime: runtime},
		GitHub: fakeGitHub{releases: testReleases()},
	}
	projects := config.Projects{Projects: []config.Project{{
		ID: "p1", Name: "项目", Repository: "owner/repo", Enabled: true,
		RetainVersions: 1, ArchitectureRegex: "(amd64)",
	}}}

	if _, err := scanner.Scan(context.Background(), projects, "", "req-scan"); err != nil {
		t.Fatal(err)
	}
	if !runtime.ConsumeSyncTaskWake("node-1") {
		t.Fatal("scan-generated task should notify active node")
	}
}
