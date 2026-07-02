package mirrorsync

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestScanFailurePersistsStateAfterContextCanceled(t *testing.T) {
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	service := Service{
		Scanner: Scanner{Store: Store{DB: db}, GitHub: cancelingGitHub{cancel: cancel}},
		Projects: NewProjectLoader("", config.Projects{Projects: []config.Project{
			testProject("p1", "owner/repo", true),
		}}),
		Interval: time.Minute,
	}

	_, err = service.Trigger(ctx, "p1", "req-cancel")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan err=%v, want context canceled", err)
	}

	var state, errText, next string
	if err := db.QueryRow(`SELECT last_scan_state, COALESCE(last_error_message, ''),
		COALESCE(next_scan_at, '') FROM project_scan_state WHERE project_id = 'p1'`).
		Scan(&state, &errText, &next); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || !strings.Contains(errText, context.Canceled.Error()) || next == "" {
		t.Fatalf("project scan state not persisted: state=%q err=%q next=%q", state, errText, next)
	}
	var scanState string
	if err := db.QueryRow(`SELECT state FROM sync_scans WHERE project_id = 'p1'`).Scan(&scanState); err != nil {
		t.Fatal(err)
	}
	if scanState != "failed" {
		t.Fatalf("sync scan state=%q, want failed", scanState)
	}
}

type cancelingGitHub struct {
	cancel context.CancelFunc
}

func (f cancelingGitHub) ListReleases(ctx context.Context, _ string) ([]GitHubRelease, error) {
	f.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}
