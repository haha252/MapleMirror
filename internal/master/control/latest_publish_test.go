package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestVerifiedPendingLatestPublishesCandidate(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedPendingLatestTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-new", "asset-new", 0, "")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID:            "task-new",
		AssetID:           "asset-new",
		Result:            "succeeded",
		LocalDigestSHA256: "sha256:new",
		SizeBytes:         20,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertControlAssetState(t, repo, "asset-old", "superseded")
	assertControlAssetState(t, repo, "asset-new", "candidate")
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-old' AND desired_state = 'remove'", 1)
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND desired_state = 'required'", 1)
}

func seedPendingLatestTarget(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	mustExecControl(t, repo.DB, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'p1', 1, 'latest', 0, '2026-01-01T00:00:00Z', 1, 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'ffmpeg.zip', 'amd64', 10,
		'https://example.invalid/old.zip', 'sha256:old', 'candidate', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 1, 'ffmpeg.zip', 'amd64', 20,
		'https://example.invalid/new.zip', 'sha256:new', 'pending', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-old', 'required', 'now')`, nodeID)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-new', 'required', 'now')`, nodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-old', 'sha256:old', 10, '2026-01-01T00:00:01Z', 'verified')`, nodeID)
}

func assertControlAssetState(t *testing.T, repo Repository, assetID, want string) {
	t.Helper()
	var got string
	if err := repo.DB.QueryRow(`SELECT service_state FROM assets WHERE id = ?`, assetID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("资产状态错误 asset_id=%s got=%q want=%q", assetID, got, want)
	}
}
