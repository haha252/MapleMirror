package control

import (
	"database/sql"
	"testing"
)

func seedAssetTarget(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	_, err := repo.DB.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'app.zip', 'amd64', 10, 'https://example.invalid',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'candidate', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-1', 'required', 'now')`, nodeID)
	if err != nil {
		t.Fatal(err)
	}
}

func mustExecControl(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}
