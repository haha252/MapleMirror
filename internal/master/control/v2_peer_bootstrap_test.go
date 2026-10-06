package control

import (
	"context"
	"sync"
	"testing"
	"time"
)

func seedPeerBootstrapTask(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	repo.runtime().SetPeerOnly(nodeID, true)
	repo.runtime().SetPeerBootstrap(nodeID, true)
	seedDownloadTask(t, repo, nodeID, "bootstrap-"+nodeID, "asset-1", 7, "")
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='pending',retry_after=NULL WHERE id=?`, "bootstrap-"+nodeID)
}

func TestV2PeerBootstrapFromLegacyCopyAndLostAcceptance(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerBootstrapTask(t, repo, session.NodeID)
	// The receiver has no public endpoint; the donor advertises no new capability.
	seedPeerNode(t, repo, "legacy-donor", "Legacy", "https://donor.example.com")
	seedVerifiedPeerAsset(t, repo, "legacy-donor", "asset-1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	task, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || !task.Bootstrap || len(task.WholeSources) != 1 {
		t.Fatalf("task=%+v ok=%v err=%v", task, ok, err)
	}
	claims, err := repo.ReplicationSigner.VerifyReplication(task.WholeSources[0].Token)
	if err != nil || claims.SourceNodeID != "legacy-donor" || claims.TargetNodeID != session.NodeID {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	replay, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || replay.AttemptID != task.AttemptID || replay.TaskID != task.TaskID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	var attempts int
	if err := repo.DB.QueryRow(`SELECT attempts FROM node_tasks WHERE id=?`, task.TaskID).Scan(&attempts); err != nil || attempts != 7 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
	// A reconnect to an old receiver must not reuse the old capability.
	repo.runtime().StartSession(Session{ID: "replacement", NodeID: session.NodeID})
	if repo.runtime().PeerBootstrap(session.NodeID) {
		t.Fatal("bootstrap capability survived reconnect")
	}
}

func TestV2PeerBootstrapWaitsWithoutSpendingAttempts(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerBootstrapTask(t, repo, session.NodeID)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if _, ok, err := repo.nextV2SyncTask(ctx, session.NodeID); ok || err != nil {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	}
	var state, attempt string
	var attempts int
	if err := repo.DB.QueryRow(`SELECT state,attempt_id,attempts FROM node_tasks WHERE id=?`, "bootstrap-"+session.NodeID).Scan(&state, &attempt, &attempts); err != nil || state != "pending" || attempt != "" || attempts != 7 {
		t.Fatalf("state=%s attempt=%s attempts=%d err=%v", state, attempt, attempts, err)
	}
	seedPeerNode(t, repo, "donor", "Donor", "https://donor.example.com")
	seedVerifiedPeerAsset(t, repo, "donor", "asset-1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	// Failed authorization must roll back the tentative claim.
	if _, ok, err := repo.nextV2SyncTask(ctx, session.NodeID); ok || err == nil {
		t.Fatalf("missing signer: ok=%v err=%v", ok, err)
	}
	repo = withReplicationSigner(t, repo)
	repo.runtime().SetPeerBootstrap(session.NodeID, false)
	if _, ok, err := repo.nextV2SyncTask(ctx, session.NodeID); ok || err != nil {
		t.Fatalf("legacy receiver: ok=%v err=%v", ok, err)
	}
	repo.runtime().SetPeerBootstrap(session.NodeID, true)
	if task, ok, err := repo.nextV2SyncTask(ctx, session.NodeID); !ok || err != nil || len(task.WholeSources) != 1 {
		t.Fatalf("recovered task=%+v ok=%v err=%v", task, ok, err)
	}
}

func TestV2PeerBootstrapClaimsOnlyOneAssetSeedConcurrently(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerBootstrapTask(t, repo, session.NodeID)
	seedPeerNode(t, repo, "receiver2", "Receiver", "https://receiver2.example.com")
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at) VALUES('receiver2','asset-1','required','now')`)
	seedPeerBootstrapTask(t, repo, "receiver2")
	seedPeerNode(t, repo, "donor", "Donor", "https://donor.example.com")
	seedVerifiedPeerAsset(t, repo, "donor", "asset-1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	var wg sync.WaitGroup
	for _, id := range []string{session.NodeID, "receiver2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, _, err := repo.nextV2SyncTask(context.Background(), id)
			if err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	var active int
	if err := repo.DB.QueryRow(`SELECT count(*) FROM node_tasks WHERE state='sent' AND asset_id='asset-1'`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active=%d err=%v", active, err)
	}
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET lease_expires_at=? WHERE state='sent'`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	// The surviving pending receiver can reclaim initialization after expiry.
	var pendingNode string
	if err := repo.DB.QueryRow(`SELECT node_id FROM node_tasks WHERE state='pending'`).Scan(&pendingNode); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.nextV2SyncTask(context.Background(), pendingNode); !ok || err != nil {
		t.Fatalf("lease failover ok=%v err=%v", ok, err)
	}
}
