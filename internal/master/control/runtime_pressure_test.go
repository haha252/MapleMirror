package control

import (
	"testing"
	"time"
)

func TestLatestRoutingPressureUsesNewestSample(t *testing.T) {
	store := NewRuntimeStore()
	old := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	fresh := time.Now().UTC().Format(time.RFC3339Nano)
	store.MarkHeartbeat("node-1", runtimeHeartbeat{
		PressureRatio: 0.2, ActiveDownloads: 1, ReportedAt: old, Valid: true,
	})
	store.MarkPressure("node-1", runtimePressureReport{
		PressureRatio: 0.8, ActiveDownloads: 9, ReportedAt: fresh, Valid: true,
	})

	got := store.LatestRoutingPressure("node-1", 2*time.Minute)
	if !got.Valid || got.PressureRatio != 0.8 || got.ActiveDownloads != 9 {
		t.Fatalf("routing pressure=%+v", got)
	}
}

func TestLatestRoutingPressureTreatsStaleSampleAsMissing(t *testing.T) {
	store := NewRuntimeStore()
	store.MarkPressure("node-1", runtimePressureReport{
		PressureRatio: 1.2, ActiveDownloads: 20,
		ReportedAt: time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano),
		Valid:      true,
	})

	got := store.LatestRoutingPressure("node-1", time.Minute)
	if got.Valid {
		t.Fatalf("stale routing pressure should be missing: %+v", got)
	}
}
