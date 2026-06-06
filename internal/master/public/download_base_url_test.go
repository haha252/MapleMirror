package public

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestCreateChallengeRejectsUnsafeDownloadBaseURL(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = 'https://node-1.example.com/prefix'
		WHERE id = 'node-1'`)

	store := Store{DB: db}
	_, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != sql.ErrNoRows {
		t.Fatalf("unsafe download base should not be routable: %v", err)
	}
}

func TestCatalogMarksUnsafeDownloadBaseURLUnavailable(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = 'https://node-1.example.com/prefix'
		WHERE id = 'node-1'`)
	store := Store{DB: db}

	projects, err := store.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Available {
		t.Fatalf("project should be unavailable with unsafe download base: %+v", projects)
	}
	assets, err := store.Assets(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Available {
		t.Fatalf("asset should be unavailable with unsafe download base: %+v", assets)
	}
	download, err := store.DownloadAsset(context.Background(), "asset-1")
	if err != nil || download.Available {
		t.Fatalf("download page should be unavailable with unsafe download base: %+v err=%v", download, err)
	}
}

func TestIssueAuthorizationSkipsUnsafeDownloadBaseURL(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	seedPublicNode(t, db, "node-2", "节点二", "https://node-2.example.com")
	mustExec(t, db, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES ('node-2', 'asset-1', 'sha256:aa', 12, '2026-01-01T00:00:01Z', 'verified')`)
	mustExec(t, db, `UPDATE nodes SET public_download_base_url = 'https://node-1.example.com/prefix',
		last_heartbeat_at = '2026-01-01T00:00:03Z' WHERE id = 'node-1'`)
	store := Store{DB: db}

	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, debug, err := store.IssueAuthorization(context.Background(), challenge, time.Minute, "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Claims.NodeID != "node-2" || debug.DownloadURL != "https://node-2.example.com/p1/v1/a.zip" {
		t.Fatalf("authorization should choose safe node: claims=%+v debug=%+v", auth.Claims, debug)
	}
}

func seedPublicNode(t *testing.T, db *sql.DB, nodeID, name, baseURL string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO nodes
		(id, public_name, state, target_bandwidth_bps, last_heartbeat_at,
		routing_ready, public_download_base_url, created_at, updated_at)
		VALUES ('`+nodeID+`', '`+name+`', 'syncing', 1, '2026-01-01T00:00:02Z',
		1, '`+baseURL+`', '2026-01-01T00:00:00Z', '2026-01-01T00:00:02Z')`)
}
