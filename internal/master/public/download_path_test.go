package public

import (
	"context"
	"testing"
)

func TestDownloadAssetByPathUsesSingleCurrentDuplicateVersion(t *testing.T) {
	db := openMaster(t)
	mustExec(t, db, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目一', 'owner/repo', 1, 2, 0, 1, 'hash', 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-old', 'p1', 1, 'latest', 0, '2026-01-01T00:00:00Z', 1, 'now')`)
	mustExec(t, db, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-new', 'p1', 2, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-old', 'rel-old', 1, 'ffmpeg.zip', 'amd64', 10,
		'https://example.test/old.zip', 'sha256:old', 'superseded', 'now')`)
	mustExec(t, db, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-new', 'rel-new', 1, 'ffmpeg.zip', 'amd64', 20,
		'https://example.test/new.zip', 'sha256:new', 'candidate', 'now')`)

	asset, err := (Store{DB: db}).DownloadAssetByPath(context.Background(), "/p1/latest/ffmpeg.zip")
	if err != nil || asset.AssetID != "asset-new" {
		t.Fatalf("重复版本路径应解析为当前候选资产 asset=%+v err=%v", asset, err)
	}
}
