package control

import (
	"context"
	"testing"

	"mirror-server/internal/protocol"
)

func TestInventoryReportIgnoresNonTargetAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		Revision: 1,
		Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new'", 0)
	assertControlAssetState(t, repo, "asset-new", "pending")
}

func TestInventoryReportIgnoresRemovedTargetAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES ('node-1', 'asset-new', 'remove', 'now')`)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		Revision: 1,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTableCount(t, repo, "node_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new'", 0)
	assertControlAssetState(t, repo, "asset-new", "pending")
}

func seedUnassignedPendingAsset(t *testing.T, repo Repository) {
	t.Helper()
	mustExecControl(t, repo.DB, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 1, 'ffmpeg.zip', 'amd64', 20,
		'https://example.invalid/new.zip', 'sha256:new', 'pending', 'now')`)
}
