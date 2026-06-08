package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestNextSyncTaskAddsVerifiedPeerFallbackSource(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerNode(t, repo, "node-2", "源节点", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	if len(task.FallbackSources) != 1 {
		t.Fatalf("expected one fallback source, got %+v", task.FallbackSources)
	}
	source := task.FallbackSources[0]
	if source.NodeID != "node-2" || source.NodeName != "源节点" {
		t.Fatalf("unexpected fallback source %+v", source)
	}
	if !strings.HasPrefix(source.DownloadURL, "https://node-2.example.com/internal/replication/") {
		t.Fatalf("unexpected fallback url %q", source.DownloadURL)
	}
	claims, err := repo.ReplicationSigner.VerifyReplication(source.Token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.AssetID != "asset-1" || claims.SourceNodeID != "node-2" || claims.TargetNodeID != session.NodeID {
		t.Fatalf("replication token not bound correctly: %+v", claims)
	}
}

func TestNextSyncTaskSkipsInvalidPeerFallbackSources(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerNode(t, repo, "node-2", "摘要错误", "https://bad-digest.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 10)
	seedPeerNode(t, repo, "node-3", "离线节点", "https://offline.example.com")
	mustExecControl(t, repo.DB, `UPDATE nodes SET state = 'offline' WHERE id = 'node-3'`)
	seedVerifiedPeerAsset(t, repo, "node-3", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	seedPeerNode(t, repo, "node-4", "缺地址", "")
	seedVerifiedPeerAsset(t, repo, "node-4", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	seedPeerNode(t, repo, "node-5", "危险地址", "https://unsafe.example.com/prefix")
	seedVerifiedPeerAsset(t, repo, "node-5", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	seedPeerNode(t, repo, "node-6", "已下线目标", "https://removed.example.com")
	seedVerifiedPeerAsset(t, repo, "node-6", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	mustExecControl(t, repo.DB, `UPDATE target_inventory SET desired_state = 'remove'
		WHERE node_id = 'node-6' AND asset_id = 'asset-1'`)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	if len(task.FallbackSources) != 0 {
		t.Fatalf("invalid peers should not be sent, got %+v", task.FallbackSources)
	}
}

func withReplicationSigner(t *testing.T, repo Repository) Repository {
	t.Helper()
	privatePath := filepath.Join(t.TempDir(), "token.key")
	publicPath := filepath.Join(t.TempDir(), "token.pub")
	if err := downloadtoken.GenerateKeyFiles(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	signer, err := downloadtoken.NewSignerFromPrivateFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	repo.ReplicationSigner = signer
	repo.ReplicationTokenTTL = time.Minute
	return repo
}

func seedPeerNode(t *testing.T, repo Repository, nodeID, name, baseURL string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := repo.DB.Exec(`INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		routing_ready, last_heartbeat_at, public_download_base_url, created_at, updated_at) VALUES
		(?, ?, ?, 'syncing', 0, 1, ?, ?, ?, ?)`, nodeID, name, "sha256:"+nodeID, now, baseURL, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after,
		status, issued_request_id, created_at) VALUES
		(?, ?, ?, ?, ?, ?, 'active', 'req', ?)`,
		"cert-"+nodeID, nodeID, nodeID, "sha256:"+nodeID,
		now, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), now)
	if err != nil {
		t.Fatal(err)
	}
}

func seedVerifiedPeerAsset(t *testing.T, repo Repository, nodeID, assetID, digest string, size int64) {
	t.Helper()
	_, err := repo.DB.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, ?, ?, 'now', 'verified')`, nodeID, assetID, digest, size)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, ?, 'required', 'now')
		ON CONFLICT(node_id, asset_id) DO UPDATE SET desired_state = 'required'`,
		nodeID, assetID)
	if err != nil {
		t.Fatal(err)
	}
}
