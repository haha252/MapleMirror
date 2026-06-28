package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestHeartbeatKeepsDisabledNodeDisabled(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `UPDATE nodes SET state = 'disabled', routing_ready = 0 WHERE id = ?`, session.NodeID)

	_, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing", MaxMirrorProjects: 3, PublicDownloadBaseURL: "https://node-1.example.com",
		Pressure: protocol.PressureSample{TargetBandwidthBPS: 104857600},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, lastHeartbeat, downloadURL string
	var ready, maxProjects int
	var targetBandwidth int64
	err = repo.DB.QueryRow(`SELECT state, routing_ready, COALESCE(last_heartbeat_at, ''),
		COALESCE(public_download_base_url, ''), target_bandwidth_bps, max_mirror_projects
		FROM nodes WHERE id = ?`, session.NodeID).
		Scan(&state, &ready, &lastHeartbeat, &downloadURL, &targetBandwidth, &maxProjects)
	if err != nil {
		t.Fatal(err)
	}
	if state != "disabled" || ready != 0 || lastHeartbeat == "" ||
		downloadURL != "https://node-1.example.com" || targetBandwidth != 104857600 || maxProjects != 3 {
		t.Fatalf("disabled heartbeat mismatch state=%s ready=%d heartbeat=%q url=%q target=%d max=%d",
			state, ready, lastHeartbeat, downloadURL, targetBandwidth, maxProjects)
	}
}
