package control

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/node/syncer"
	"mirror-server/internal/storage"
)

func TestV2PeerBootstrapIntegrationCompletesAndUnlocksSwarm(t *testing.T) {
	ctx := context.Background()
	content := []byte("abcdef")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `UPDATE assets SET size_bytes=6,digest_sha256=? WHERE id='asset-1'`, digest)
	mustExecControl(t, repo.DB, `UPDATE nodes SET public_download_base_url='',state='online',last_heartbeat_at='now' WHERE id=?`, session.NodeID)
	seedPeerBootstrapTask(t, repo, session.NodeID)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := repo.ReplicationSigner.VerifyReplication(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || claims.TargetNodeID != session.NodeID || claims.AssetID != "asset-1" {
			t.Errorf("claims=%+v err=%v", claims, err)
			http.Error(w, "unauthorized", 401)
			return
		}
		_, _ = w.Write(content)
	}))
	defer peer.Close()
	seedPeerNode(t, repo, "donor", "Legacy donor", peer.URL)
	seedVerifiedPeerAsset(t, repo, "donor", "asset-1", digest, 6)
	task, ok, err := repo.nextV2SyncTask(ctx, session.NodeID)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if _, err := repo.acceptV2Task(ctx, session, task.TaskID, task.AttemptID); err != nil {
		t.Fatal(err)
	}
	nodeDB, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeDB.Close()
	result, manifest := (syncer.Executor{DB: nodeDB, Storage: t.TempDir(), TempDir: t.TempDir(), Client: peer.Client(), ForcePeerDownload: true, AllowPrivateSourceURLs: true}).ExecuteV2(ctx, task)
	if result.Result != "succeeded" || manifest == nil {
		t.Fatalf("result=%+v manifest=%+v", result, manifest)
	}
	ack, err := repo.AcceptV2Manifest(ctx, session.NodeID, *manifest)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	// Replay after lost manifest ACK is idempotent.
	ack, err = repo.AcceptV2Manifest(ctx, session.NodeID, *manifest)
	if err != nil || ack.Status != "accepted" {
		t.Fatalf("duplicate ack=%+v err=%v", ack, err)
	}
	if _, err := repo.acceptV2SyncResult(ctx, session, result); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.acceptV2SyncResult(ctx, session, result); err != nil {
		t.Fatal(err)
	}
	assertNodeReady(t, repo, session.NodeID, 1)
	var unfinished, missing int
	if err := repo.DB.QueryRow(`SELECT count(*) FROM node_tasks WHERE node_id=? AND state IN ('pending','sent','running','retry_wait','failed')`, session.NodeID).Scan(&unfinished); err != nil {
		t.Fatal(err)
	}
	if err := repo.DB.QueryRow(`SELECT count(*) FROM target_inventory ti LEFT JOIN node_inventory ni ON ni.node_id=ti.node_id AND ni.asset_id=ti.asset_id WHERE ti.node_id=? AND ti.desired_state='required' AND (ni.asset_id IS NULL OR ni.state!='verified')`, session.NodeID).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if unfinished != 0 || missing != 0 {
		t.Fatalf("unfinished=%d missing=%d", unfinished, missing)
	}
	seedPeerNode(t, repo, "next-receiver", "Next receiver", "https://next.example.com")
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at) VALUES('next-receiver','asset-1','required','now')`)
	seedDownloadTask(t, repo, "next-receiver", "next-task", "asset-1", 0, "")
	repo.runtime().SetPeerOnly("next-receiver", true)
	next, ok, err := repo.nextV2SyncTask(ctx, "next-receiver")
	if err != nil || !ok || next.Bootstrap || next.Manifest == nil || next.Manifest.ManifestID != manifest.ManifestID {
		t.Fatalf("next=%+v ok=%v err=%v", next, ok, err)
	}
}
