package control

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/storage"
)

func inventoryBatchFixture(t testing.TB, count int) (Repository, Session, []protocol.InventoryItem) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := Repository{DB: db, Runtime: NewRuntimeStore()}
	for _, stmt := range []string{
		`INSERT INTO nodes (id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		 routing_ready, created_at, updated_at) VALUES ('node-1','node','sha256:aa','syncing',0,0,'now','now')`,
		`INSERT INTO projects (id,name,repository,enabled,retain_versions,include_prerelease,
		 download_multiplier,config_hash,updated_at) VALUES ('p1','project','owner/repo',1,1,0,1,'hash','now')`,
		`INSERT INTO releases (id,project_id,github_release_id,tag_name,prerelease,published_at,selected,created_at)
		 VALUES ('rel-1','p1',1,'v1',0,'now',1,'now')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	items := make([]protocol.InventoryItem, count)
	for i := range items {
		id := fmt.Sprintf("asset-%04d", i)
		items[i] = protocol.InventoryItem{AssetID: id, DigestSHA256: "sha256:aa", SizeBytes: 10, LocalState: "verified"}
		if _, err := tx.Exec(`INSERT INTO assets
		 (id,release_id,github_asset_id,file_name,architecture,size_bytes,source_url,digest_sha256,service_state,created_at)
		 VALUES (?,'rel-1',?,?,'amd64',10,'https://example.invalid','sha256:aa','candidate','now')`, id, i+1, id+".zip"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO target_inventory VALUES ('node-1',?,'required','now')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return repo, Session{NodeID: "node-1", RequestID: "req"}, items
}

func inventoryBatchSnapshot(t *testing.T, tx *sql.Tx) []string {
	t.Helper()
	rows, err := tx.Query(`SELECT asset_id,state,local_digest_sha256,size_bytes,verified_at FROM node_inventory ORDER BY asset_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var snapshot []string
	for rows.Next() {
		var id, state, digest, verified string
		var size int64
		if err := rows.Scan(&id, &state, &digest, &size, &verified); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("%s:%s:%s:%d:%s", id, state, digest, size, verified))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestInventoryBatchMatchesSequentialAcceptance(t *testing.T) {
	repo, session, items := inventoryBatchFixture(t, 1000)
	mustExecControl(t, repo.DB, `UPDATE projects SET enabled=0`)
	mustExecControl(t, repo.DB, `UPDATE releases SET selected=0`)
	mustExecControl(t, repo.DB, `DELETE FROM target_inventory WHERE asset_id LIKE '%3'`)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		SELECT 'node-1',id,'sha256:old',10,'before','stale' FROM assets`)
	states := []string{"verified", "missing", "removed", "superseded", "mismatch", "other"}
	for i := range items {
		items[i].LocalState = states[i%len(states)]
		if i%4 == 0 {
			items[i].DigestSHA256 = "sha256:old"
		}
	}
	// Duplicate IDs across lookup and write boundaries must see earlier mutations.
	items = append(items, items[0], protocol.InventoryItem{AssetID: "unknown"},
		protocol.InventoryItem{AssetID: items[1].AssetID, DigestSHA256: "sha256:old", SizeBytes: 10})
	ctx := context.Background()
	tx, err := repo.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if _, err := acceptInventoryItem(ctx, tx, session.NodeID, item, "after"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	want := inventoryBatchSnapshot(t, tx)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = repo.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	projects, quarantined, err := repo.acceptInventoryItems(ctx, tx, session, items, "after")
	if err != nil || quarantined || len(projects) != 0 {
		t.Fatalf("projects=%v quarantined=%v err=%v", projects, quarantined, err)
	}
	if got := inventoryBatchSnapshot(t, tx); !reflect.DeepEqual(got, want) {
		t.Fatal("batch inventory differs from sequential acceptance")
	}
}

func TestInventoryBatchStopsAtPublicMismatch(t *testing.T) {
	repo, session, items := inventoryBatchFixture(t, 153)
	items[151].DigestSHA256 = "sha256:wrong"
	tx, err := repo.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	projects, quarantined, err := repo.acceptInventoryItems(context.Background(), tx, session, items, "after")
	if err != nil || !quarantined || len(projects) != 1 {
		t.Fatalf("projects=%v quarantined=%v err=%v", projects, quarantined, err)
	}
	var count int
	var state, nodeState string
	if err := tx.QueryRow(`SELECT COUNT(*) FROM node_inventory`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT state FROM node_inventory WHERE asset_id=?`, items[151].AssetID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT state FROM nodes WHERE id=?`, session.NodeID).Scan(&nodeState); err != nil {
		t.Fatal(err)
	}
	if count != 152 || state != "mismatch" || nodeState != "disabled" {
		t.Fatalf("count=%d mismatch=%s node=%s", count, state, nodeState)
	}
}

func TestV2InventoryBatchFailureRollsBackReceiptAndEarlierWrites(t *testing.T) {
	repo, session, items := inventoryBatchFixture(t, 153)
	mustExecControl(t, repo.DB, `CREATE TRIGGER reject_inventory BEFORE INSERT ON node_inventory
		WHEN NEW.asset_id='asset-0151' BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
	segment := protocolv2.InventorySnapshotSegment{Revision: 1, GeneratedAt: time.Now()}
	for _, item := range items {
		segment.Items = append(segment.Items, protocolv2.InventoryItem{AssetID: item.AssetID,
			DigestSHA256: item.DigestSHA256, SizeBytes: item.SizeBytes, LocalState: item.LocalState})
	}
	if _, err := repo.AcceptV2InventorySegment(context.Background(), session, segment, "segment-1"); err == nil {
		t.Fatal("expected write failure")
	}
	for _, table := range []string{"node_inventory", "node_inventory_v2_segments", "node_inventory_reports"} {
		assertTableCount(t, repo, table, "1=1", 0)
	}
	mustExecControl(t, repo.DB, `DROP TRIGGER reject_inventory`)
	if _, err := repo.AcceptV2InventorySegment(context.Background(), session, segment, "segment-1"); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	assertTableCount(t, repo, "node_inventory", "1=1", 153)
}

func BenchmarkInventoryItems1000(b *testing.B) {
	for _, batch := range []bool{false, true} {
		name := "sequential"
		if batch {
			name = "batch"
		}
		b.Run(name, func(b *testing.B) {
			repo, session, items := inventoryBatchFixture(b, 1000)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tx, err := repo.DB.BeginTx(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}
				if batch {
					_, _, err = repo.acceptInventoryItems(ctx, tx, session, items, "after")
				} else {
					projects := map[string]struct{}{}
					for _, item := range items {
						if _, err = acceptInventoryItem(ctx, tx, session.NodeID, item, "after"); err != nil {
							break
						}
						if err = recordVerifiedAssetProject(ctx, tx, item.AssetID, projects); err != nil {
							break
						}
					}
				}
				_ = tx.Rollback()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
