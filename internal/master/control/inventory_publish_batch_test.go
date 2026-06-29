package control

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/protocol"
	"mirror-server/internal/storage"
)

func TestCompleteInventoryBatchesVerifiedAssetsByProject(t *testing.T) {
	repo, counter, closeDB := testRepoWithSQLCounter(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedSecondAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	counter.Reset()

	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-batched", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{
			{
				AssetID: "asset-1", SizeBytes: 10,
				DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				LocalState:   "verified",
			},
			{
				AssetID: "asset-2", SizeBytes: 20,
				DigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				LocalState:   "verified",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.SelectAllNodes(); got != 0 {
		t.Fatalf("verified asset publishing should not reconcile all nodes, got %d", got)
	}
	assertTableCount(t, repo, "node_project_assignments",
		"node_id = 'node-1' AND project_id = 'p1'", 1)
	var ready int
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 1 {
		t.Fatalf("batched inventory should still mark node ready, got %d", ready)
	}
}

func TestCompleteInventoryBatchesVerifiedAssetsPerProject(t *testing.T) {
	repo, counter, closeDB := testRepoWithSQLCounter(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedOtherProjectTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	counter.Reset()

	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-projects", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{
			{
				AssetID: "asset-1", SizeBytes: 10,
				DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				LocalState:   "verified",
			},
			{
				AssetID: "asset-3", SizeBytes: 30,
				DigestSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				LocalState:   "verified",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := counter.SelectAllNodes(); got != 0 {
		t.Fatalf("cross-project publishing should not reconcile all nodes, got %d", got)
	}
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'required'", 1)
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-3' AND desired_state = 'required'", 1)
}

type sqlSelectCounter struct {
	mu             sync.Mutex
	selectAllNodes int
}

func (c *sqlSelectCounter) Log(_ context.Context, message string, attrs ...slog.Attr) {
	if message != "SQL 耗时" {
		return
	}
	for _, attr := range attrs {
		if attr.Key == "sql" && attr.Value.String() == "SELECT id FROM nodes" {
			c.mu.Lock()
			c.selectAllNodes++
			c.mu.Unlock()
			return
		}
	}
}

func (c *sqlSelectCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.selectAllNodes = 0
}

func (c *sqlSelectCounter) SelectAllNodes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selectAllNodes
}

func testRepoWithSQLCounter(t *testing.T) (Repository, *sqlSelectCounter, func()) {
	t.Helper()
	wal := true
	counter := &sqlSelectCounter{}
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	}, storage.WithSQLDebugLogger(counter.Log))
	if err != nil {
		t.Fatal(err)
	}
	return Repository{DB: db, Runtime: NewRuntimeStore()}, counter, func() { _ = db.Close() }
}

func seedOtherProjectTarget(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	mustExecControl(t, repo.DB, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p2', '项目二', 'owner/repo2', 1, 1, 0, 1, 'hash2', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-2', 'p2', 2, 'v1', 0, 'now', 1, 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-3', 'rel-2', 3, 'app3.zip', 'amd64', 30, 'https://example.invalid',
		'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
		'candidate', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-3', 'required', 'now')`, nodeID)
}
