package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestWALCheckpointMonitorFailsWhenPinnedWALGrowsPastThreshold(t *testing.T) {
	var monitor walCheckpointMonitor
	const threshold = int64(256 * 1024 * 1024)
	first := storage.WALCheckpointResult{
		LogFrames: 1000, CheckedFrames: 940, WALSizeBytes: threshold - 1,
	}
	if err := monitor.Observe(first, threshold); err != nil {
		t.Fatalf("first incomplete checkpoint: %v", err)
	}
	second := storage.WALCheckpointResult{
		LogFrames: 70000, CheckedFrames: 940, WALSizeBytes: threshold,
	}
	if err := monitor.Observe(second, threshold); err == nil {
		t.Fatal("pinned WAL at threshold did not fail")
	}
}

func TestWALCheckpointMonitorAllowsProgressAndRecovery(t *testing.T) {
	var monitor walCheckpointMonitor
	const threshold = int64(256 * 1024 * 1024)
	results := []storage.WALCheckpointResult{
		{LogFrames: 1000, CheckedFrames: 940, WALSizeBytes: threshold},
		{LogFrames: 2000, CheckedFrames: 1500, WALSizeBytes: threshold * 2},
		{LogFrames: 2000, CheckedFrames: 2000, WALSizeBytes: threshold * 2},
		{LogFrames: 3000, CheckedFrames: 2500, WALSizeBytes: threshold * 2},
	}
	for _, result := range results {
		if err := monitor.Observe(result, threshold); err != nil {
			t.Fatalf("progressing checkpoint %+v failed: %v", result, err)
		}
	}
}

func TestMaintainOnlineDataPrunesExpiredDownloadHistory(t *testing.T) {
	wal := false
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insert := func(id string, issued time.Time) {
		when := issued.UTC().Format(time.RFC3339Nano)
		_, err := db.Exec(`INSERT INTO download_history
			(authorization_id, client_prefix_key, source_kind, project_id, project_name,
			asset_id, file_name, version, system, architecture, node_id, node_name,
			issued_at, expires_at, status, request_id, updated_at)
			VALUES (?, '192.0.2.1/32', 'web', 'p1', '项目一', 'asset-1', 'a.zip',
			'v1', '', 'amd64', 'node-1', '节点一', ?, ?, 'issued', ?, ?)`,
			id, when, when, "req-"+id, when)
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	insert("old", now.AddDate(0, 0, -8))
	insert("fresh", now.AddDate(0, 0, -1))
	if err := maintainOnlineData(context.Background(), db, 7); err != nil {
		t.Fatal(err)
	}
	var oldCount, freshCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_history WHERE authorization_id = 'old'`).
		Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM download_history WHERE authorization_id = 'fresh'`).
		Scan(&freshCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 || freshCount != 1 {
		t.Fatalf("history retention old=%d fresh=%d", oldCount, freshCount)
	}
}
