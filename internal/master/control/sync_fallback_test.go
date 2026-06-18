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

func TestNextSyncTaskAddsShardedPeerFallbackTokensForLargeAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	size := int64(defaultPeerFallbackMinSizeBytes + 8)
	mustExecControl(t, repo.DB, `UPDATE assets SET size_bytes = ? WHERE id = 'asset-1'`, size)
	seedPeerNode(t, repo, "node-2", "源节点", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", size)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	source := task.FallbackSources[0]
	if len(source.Parts) != defaultPeerFallbackWorkers {
		t.Fatalf("expected %d parts, got %+v", defaultPeerFallbackWorkers, source.Parts)
	}
	var next int64
	for _, part := range source.Parts {
		if part.RangeStart != next || part.RangeEnd < part.RangeStart {
			t.Fatalf("unexpected part range: %+v next=%d", part, next)
		}
		claims, err := repo.ReplicationSigner.VerifyReplication(part.Token)
		if err != nil {
			t.Fatal(err)
		}
		if claims.RangeStart != part.RangeStart || claims.RangeEnd != part.RangeEnd ||
			claims.AssetID != "asset-1" || claims.SourceNodeID != "node-2" || claims.TargetNodeID != session.NodeID {
			t.Fatalf("part token not bound correctly: part=%+v claims=%+v", part, claims)
		}
		next = part.RangeEnd + 1
	}
	if next != size {
		t.Fatalf("parts should cover asset size, got %d want %d", next, size)
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

func TestNextSyncTaskDemotesPublicProbeBlockedPeerFallbackSource(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	repo.PublicProbeNetworkFailures = 5
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerNode(t, repo, "node-2", "公网不可达", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	markPeerPublicProbeFailures(t, repo, "node-2", 5)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	if len(task.FallbackSources) != 1 || task.FallbackSources[0].NodeID != "node-2" {
		t.Fatalf("public-probe blocked peer should still be usable for sync, got %+v", task.FallbackSources)
	}
}

func TestNextSyncTaskKeepsPeerFallbackSourceBelowPublicProbeThreshold(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	repo.PublicProbeNetworkFailures = 5
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerNode(t, repo, "node-2", "偶发失败", "https://node-2.example.com")
	seedVerifiedPeerAsset(t, repo, "node-2", "asset-1",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	markPeerPublicProbeFailures(t, repo, "node-2", 4)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	if len(task.FallbackSources) != 1 || task.FallbackSources[0].NodeID != "node-2" {
		t.Fatalf("peer below public-probe threshold should be sent, got %+v", task.FallbackSources)
	}
}

func TestReplicationTokenTTLIsCapped(t *testing.T) {
	repo := Repository{}
	if got := repo.replicationTokenTTL(); got != maxReplicationTokenTTL {
		t.Fatalf("default replication ttl=%s want=%s", got, maxReplicationTokenTTL)
	}
	repo.ReplicationTokenTTL = 30 * time.Second
	if got := repo.replicationTokenTTL(); got != 30*time.Second {
		t.Fatalf("short replication ttl=%s", got)
	}
	repo.ReplicationTokenTTL = 15 * time.Minute
	if got := repo.replicationTokenTTL(); got != maxReplicationTokenTTL {
		t.Fatalf("long replication ttl should be capped, got=%s", got)
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

func markPeerPublicProbeFailures(t *testing.T, repo Repository, nodeID string, failures int) {
	t.Helper()
	mustExecControl(t, repo.DB, `UPDATE nodes SET public_probe_network_failures = ?,
		last_public_probe_result = 'network_error',
		last_public_probe_error = 'i/o timeout' WHERE id = ?`, failures, nodeID)
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
