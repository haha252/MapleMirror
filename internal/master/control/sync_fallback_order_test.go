package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestNextSyncTaskOrdersPeerFallbackByHealth(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	repo = withReplicationSigner(t, repo)
	repo.PublicProbeNetworkFailures = 5
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedPeerNode(t, repo, "node-a", "A-公网失败", "https://node-a.example.com")
	seedPeerNode(t, repo, "node-b", "B-新鲜成功", "https://node-b.example.com")
	seedPeerNode(t, repo, "node-c", "C-老成功", "https://node-c.example.com")
	for _, id := range []string{"node-a", "node-b", "node-c"} {
		seedVerifiedPeerAsset(t, repo, id, "asset-1",
			"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10)
	}
	markPeerPublicProbeFailures(t, repo, "node-a", 5)
	markPeerProbeSuccess(t, repo, "node-b", time.Now().UTC())
	markPeerProbeSuccess(t, repo, "node-c", time.Now().UTC().Add(-time.Hour))
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok {
		t.Fatalf("expected sync task, ok=%v err=%v", ok, err)
	}
	got := fallbackNodeIDs(task.FallbackSources)
	want := []string{"node-b", "node-c", "node-a"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("fallback order=%v want prefix=%v", got, want)
		}
	}
}

func markPeerProbeSuccess(t *testing.T, repo Repository, nodeID string, at time.Time) {
	t.Helper()
	mustExecControl(t, repo.DB, `UPDATE nodes SET public_probe_network_failures = 0,
		last_public_probe_result = 'success', last_public_probe_error = '',
		last_public_probe_at = ? WHERE id = ?`, at.Format(time.RFC3339Nano), nodeID)
}

func fallbackNodeIDs(sources []protocol.SyncFallbackSource) []string {
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.NodeID)
	}
	return out
}
