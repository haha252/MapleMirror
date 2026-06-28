package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestPressureReportUpdatesTargetBandwidthRuntime(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	report := protocol.PressureReport{
		SampleWindowSeconds: 10,
		TargetBandwidthBPS:  104857600,
		ActualBandwidthBPS:  52428800,
		FreeBytes:           1,
	}
	if _, err := repo.AcceptPressureReport(context.Background(), session, 1, report); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := repo.DB.QueryRow(`SELECT target_bandwidth_bps FROM nodes WHERE id = ?`,
		session.NodeID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != report.TargetBandwidthBPS {
		t.Fatalf("stored target bandwidth = %d", stored)
	}
	latest, err := repo.LatestPressureReport(context.Background(), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if latest["target_bandwidth_bps"] != report.TargetBandwidthBPS ||
		latest["actual_bandwidth_bps"] != report.ActualBandwidthBPS {
		t.Fatalf("latest pressure report missing bandwidth fields: %+v", latest)
	}
	if latest["pressure_ratio"] != 0.5 {
		t.Fatalf("pressure ratio = %+v", latest["pressure_ratio"])
	}
}

func TestPressureReportKeepsDisabledNodeDisabled(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `UPDATE nodes SET state = 'disabled', routing_ready = 0 WHERE id = ?`, session.NodeID)
	report := protocol.PressureReport{
		SampleWindowSeconds: 10,
		TargetBandwidthBPS:  104857600,
		ActualBandwidthBPS:  52428800,
		FreeBytes:           1,
		MaxMirrorProjects:   4,
	}
	if _, err := repo.AcceptPressureReport(context.Background(), session, 1, report); err != nil {
		t.Fatal(err)
	}
	var state, lastHeartbeat string
	var ready, maxProjects int
	var targetBandwidth int64
	err := repo.DB.QueryRow(`SELECT state, routing_ready, COALESCE(last_heartbeat_at, ''),
		target_bandwidth_bps, max_mirror_projects FROM nodes WHERE id = ?`,
		session.NodeID).Scan(&state, &ready, &lastHeartbeat, &targetBandwidth, &maxProjects)
	if err != nil {
		t.Fatal(err)
	}
	if state != "disabled" || ready != 0 || lastHeartbeat == "" ||
		targetBandwidth != report.TargetBandwidthBPS || maxProjects != report.MaxMirrorProjects {
		t.Fatalf("disabled pressure mismatch state=%s ready=%d heartbeat=%q target=%d max=%d",
			state, ready, lastHeartbeat, targetBandwidth, maxProjects)
	}
}
