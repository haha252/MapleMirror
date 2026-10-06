package control

import (
	"context"
	"testing"
)

func TestRepairNodeReadinessCorrectsHistoricalFlag(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `UPDATE nodes SET routing_ready=1,state='online',last_heartbeat_at='now' WHERE id=?`, session.NodeID)
	if err := repo.RepairNodeReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNodeReady(t, repo, session.NodeID, 0)
	seedVerifiedPeerAsset(t, repo, session.NodeID, "asset-1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	if err := repo.RepairNodeReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNodeReady(t, repo, session.NodeID, 1)
	seedDownloadTask(t, repo, session.NodeID, "unfinished", "asset-1", 0, "")
	if err := repo.RepairNodeReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNodeReady(t, repo, session.NodeID, 0)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='succeeded' WHERE id='unfinished'`)
	mustExecControl(t, repo.DB, `UPDATE node_inventory SET local_digest_sha256='wrong' WHERE node_id=?`, session.NodeID)
	if err := repo.RepairNodeReadiness(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertNodeReady(t, repo, session.NodeID, 0)
}

func assertNodeReady(t *testing.T, repo Repository, nodeID string, want int) {
	t.Helper()
	var ready int
	if err := repo.DB.QueryRow(`SELECT routing_ready FROM nodes WHERE id=?`, nodeID).Scan(&ready); err != nil || ready != want {
		t.Fatalf("ready=%d want=%d err=%v", ready, want, err)
	}
}
