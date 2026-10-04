package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	masterupgrades "mirror-server/internal/storage/upgrades/master"
)

var onlineQueryIndexNames = []string{
	"idx_download_authorizations_pending_delivery", "idx_target_inventory_required_counts",
	"idx_node_inventory_live_counts", "idx_node_tasks_live_counts", "idx_node_availability_rollups_counts",
}

func TestMasterV23IndexesUpgradeExistingDatabaseWithoutChangingRecords(t *testing.T) {
	wal := false
	cfg := config.Database{Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal}
	db, err := OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range onlineQueryIndexNames {
		if _, err := db.Exec(`DROP INDEX ` + index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE database_version SET version=22 WHERE kind='master'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nodes (id,public_name,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
		VALUES ('kept','kept','offline',0,0,'before','before')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	db, err = OpenMaster(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDBVersion(t, db, "master", 23)
	for _, index := range onlineQueryIndexNames {
		assertIndex(t, db, index)
	}
	var updated string
	if err := db.QueryRow(`SELECT updated_at FROM nodes WHERE id='kept'`).Scan(&updated); err != nil || updated != "before" {
		t.Fatalf("existing row changed: %s err=%v", updated, err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := masterupgrades.V22ToV23(context.Background(), tx); err != nil {
		t.Fatalf("upgrade not idempotent: %v", err)
	}
}

func TestOnlineQueryPlansAvoidHistoricalScansAndAuthorizationSort(t *testing.T) {
	wal := false
	db, err := OpenMaster(config.Database{Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queries := []string{
		`SELECT id FROM download_authorizations WHERE node_id='n' AND token_hash!='' AND delivered_at=''
		 AND status IN ('issued','active') ORDER BY issued_at,id LIMIT 1`,
		`SELECT node_id,COUNT(*) FROM target_inventory WHERE desired_state='required' GROUP BY node_id`,
		`SELECT node_id,SUM(state='verified') FROM node_inventory WHERE state IN ('verified','mismatch') GROUP BY node_id`,
		`SELECT node_id,SUM(state='running') FROM node_tasks WHERE state IN ('pending','sent','running','retry_wait','failed') GROUP BY node_id`,
		`SELECT SUM(total_samples),SUM(ok_samples) FROM node_availability_rollups WHERE node_id='n' AND bucket_start>='2026-10-01'`,
	}
	for i, query := range queries {
		rows, err := db.Query(`EXPLAIN QUERY PLAN ` + query)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, onlineQueryIndexNames[i]) || strings.Contains(text, "TEMP B-TREE") {
			t.Fatalf("historical scan/sort remains: %s", text)
		}
	}
}

func BenchmarkPendingAuthorizationLookup(b *testing.B) {
	for _, indexed := range []bool{false, true} {
		name := "legacy"
		if indexed {
			name = "partial"
		}
		b.Run(name, func(b *testing.B) {
			db, err := sql.Open("sqlite", filepath.Join(b.TempDir(), "queue.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			for _, query := range []string{
				`CREATE TABLE download_authorizations (id TEXT PRIMARY KEY,node_id TEXT,token_hash TEXT,delivered_at TEXT,status TEXT,issued_at TEXT)`,
				`CREATE INDEX legacy_delivery ON download_authorizations(node_id,delivered_at,issued_at)`,
				`WITH RECURSIVE seq(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM seq WHERE i<10000)
				 INSERT INTO download_authorizations SELECT 'a-'||i,'n','hash','','revoked','now' FROM seq`,
			} {
				if _, err := db.Exec(query); err != nil {
					b.Fatal(err)
				}
			}
			if indexed {
				if _, err := db.Exec(`CREATE INDEX pending_delivery ON download_authorizations(node_id,issued_at,id)
				 WHERE token_hash!='' AND delivered_at='' AND status IN ('issued','active')`); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for range b.N {
				var id string
				err := db.QueryRow(`SELECT id FROM download_authorizations WHERE node_id='n' AND token_hash!='' AND delivered_at=''
				 AND status IN ('issued','active') ORDER BY issued_at,id LIMIT 1`).Scan(&id)
				if err != sql.ErrNoRows {
					b.Fatalf("err=%v", err)
				}
			}
		})
	}
}
