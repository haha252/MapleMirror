package control

import (
	"context"
	"testing"
	"time"
)

func TestV2ManifestBootstrapAllowsOnlyOneActiveSeedAndFailsOver(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO nodes
		(id,public_name,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
		VALUES('node-2','节点二','online',0,1,?,?)`, now, now)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at)
		VALUES('node-2','asset-1','required',?)`, now)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at)
		VALUES('seed-1',?,'asset-1','asset_download','pending','req-1',?,?)`, session.NodeID, now, now)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at)
		VALUES('seed-2','node-2','asset-1','asset_download','pending','req-2',?,?)`, now, now)

	first, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || !first.Bootstrap {
		t.Fatalf("first seed task=%+v ok=%v err=%v", first, ok, err)
	}
	if _, ok, err := repo.nextV2SyncTask(context.Background(), "node-2"); err != nil || ok {
		t.Fatalf("second seed must wait while first lease is active: ok=%v err=%v", ok, err)
	}

	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='retry_wait',attempt_id='',lease_expires_at=NULL,retry_after=NULL WHERE id='seed-1'`)
	second, ok, err := repo.nextV2SyncTask(context.Background(), "node-2")
	if err != nil || !ok || !second.Bootstrap {
		t.Fatalf("bootstrap did not fail over: task=%+v ok=%v err=%v", second, ok, err)
	}
}

func TestV2PeerOnlyNodeDoesNotClaimBootstrap(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO nodes
		(id,public_name,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
		VALUES('node-peer','Peer only','online',0,1,?,?)`, now, now)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at)
		VALUES('node-peer','asset-1','required',?)`, now)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at)
		VALUES('origin-seed',?,'asset-1','asset_download','pending','req-origin',?,?)`, session.NodeID, now, now)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at)
		VALUES('peer-wait','node-peer','asset-1','asset_download','pending','req-peer',?,?)`, now, now)

	repo.runtime().SetPeerOnly("node-peer", true)
	if task, ok, err := repo.nextV2SyncTask(context.Background(), "node-peer"); err != nil || ok {
		t.Fatalf("peer-only node must wait for manifest: task=%+v ok=%v err=%v", task, ok, err)
	}
	task, ok, err := repo.nextV2SyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || !task.Bootstrap || task.TaskID != "origin-seed" {
		t.Fatalf("origin-capable node should claim bootstrap: task=%+v ok=%v err=%v", task, ok, err)
	}
}
