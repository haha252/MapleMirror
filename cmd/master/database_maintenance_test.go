package main

import (
	"testing"

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
