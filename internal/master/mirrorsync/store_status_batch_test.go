package mirrorsync

import (
	"context"
	"reflect"
	"testing"

	mastercontrol "mirror-server/internal/master/control"
)

func TestSyncStatusesMatchesSingleNodeDiagnosis(t *testing.T) {
	db := openScannerTestDB(t)
	defer db.Close()
	seedNode(t, db)
	queries := []string{
		`INSERT INTO nodes (id, public_name, state, routing_ready, target_bandwidth_bps, created_at, updated_at)
			VALUES ('offline', '离线节点', 'offline', 0, 1, 'now', 'now'), ('ready', '就绪节点', 'online', 1, 1, 'now', 'now')`,
		`INSERT INTO projects (id, name, repository, enabled, retain_versions, include_prerelease, download_multiplier, config_hash, updated_at)
			VALUES ('p', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`,
		`INSERT INTO releases (id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
			VALUES ('r', 'p', 1, 'v1', 0, 'now', 1, 'now')`,
		`INSERT INTO assets (id, release_id, github_asset_id, file_name, architecture, size_bytes, source_url, digest_sha256, service_state, created_at)
			VALUES ('a1', 'r', 1, 'a1.zip', 'amd64', 10, 'https://example.test/a1', 'sha', 'candidate', 'now'),
			('a2', 'r', 2, 'a2.zip', 'amd64', 10, 'https://example.test/a2', 'sha', 'candidate', 'now'),
			('old', 'r', 3, 'old.zip', 'amd64', 10, 'https://example.test/old', 'sha', 'candidate', 'now')`,
		`INSERT INTO target_inventory (node_id, asset_id, desired_state, updated_at)
			VALUES ('node-1', 'a1', 'required', 'now'), ('node-1', 'a2', 'required', 'now'), ('node-1', 'old', 'remove', 'now')`,
		`INSERT INTO node_inventory (node_id, asset_id, local_digest_sha256, size_bytes, state, verified_at)
			VALUES ('node-1', 'a1', 'sha', 10, 'verified', 'now'), ('node-1', 'a2', 'bad', 10, 'mismatch', 'now'),
			('node-1', 'old', 'sha', 10, 'verified', 'now')`,
	}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"pending", "sent", "running", "retry_wait", "failed", "succeeded", "cancelled"} {
		if _, err := db.Exec(`INSERT INTO node_tasks (id, node_id, task_type, state, request_id, created_at)
			VALUES (?, 'node-1', 'inventory_reconcile', ?, 'req', 'now')`, state, state); err != nil {
			t.Fatal(err)
		}
	}
	for _, runtime := range []*mastercontrol.RuntimeStore{nil, mastercontrol.NewRuntimeStore()} {
		if runtime != nil {
			runtime.StartSession(mastercontrol.Session{ID: "active", NodeID: "node-1"})
		}
		store := Store{DB: db, Runtime: runtime}
		items, err := store.SyncStatuses(context.Background())
		if err != nil || len(items) != 3 {
			t.Fatalf("items=%+v err=%v", items, err)
		}
		for _, item := range items {
			one, err := store.SyncStatus(context.Background(), item.NodeID)
			if err != nil || !reflect.DeepEqual(item, one) {
				t.Fatalf("batch differs from single diagnosis: batch=%+v single=%+v err=%v", item, one, err)
			}
			if item.NodeID == "node-1" && (item.RequiredAssets != 2 || item.MissingAssets != 1 || item.VerifiedAssets != 2 || item.OutstandingTasks != 5) {
				t.Fatalf("target or task aggregation incorrect: %+v", item)
			}
		}
	}
}

func TestSyncStatusesDoesNotHideDatabaseFailure(t *testing.T) {
	db := openScannerTestDB(t)
	db.Close()
	if _, err := (Store{DB: db}).SyncStatuses(context.Background()); err == nil {
		t.Fatal("failed query must not appear as zero healthy counts")
	}
}
