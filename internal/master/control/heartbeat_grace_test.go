package control

import (
	"context"
	"testing"
	"time"
)

func TestSweepOfflineDelaysNodeWithinGraceWindow(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().CloseSession(session.ID)
	_, err := repo.DB.Exec(`UPDATE nodes SET routing_ready = 1, last_heartbeat_at = ?
		WHERE id = ?`, time.Now().UTC().Add(-2*time.Second).Format(time.RFC3339Nano), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.SweepOffline(context.Background(), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfflineNodes != 0 || result.DelayedNodes != 1 {
		t.Fatalf("软超时节点应只计为延迟：result=%+v", result)
	}
	assertNodeRouteState(t, repo, session.NodeID, 1, "online")
}

func TestSweepOfflineMasksRoutingAfterGraceWindow(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().CloseSession(session.ID)
	_, err := repo.DB.Exec(`UPDATE nodes SET routing_ready = 1, last_heartbeat_at = ?
		WHERE id = ?`, time.Now().UTC().Add(-10*time.Second).Format(time.RFC3339Nano), session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.SweepOffline(context.Background(), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.OfflineNodes != 1 || result.DelayedNodes != 0 {
		t.Fatalf("硬超时节点应离线：result=%+v", result)
	}
	assertNodeRouteState(t, repo, session.NodeID, 0, "offline")
}

func assertNodeRouteState(t *testing.T, repo Repository, nodeID string, wantReady int, wantState string) {
	t.Helper()
	var ready int
	var state string
	_ = repo.DB.QueryRow("SELECT routing_ready, state FROM nodes WHERE id = ?", nodeID).Scan(&ready, &state)
	if ready != wantReady || state != wantState {
		t.Fatalf("节点状态不符合预期 ready=%d state=%s", ready, state)
	}
}
