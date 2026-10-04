package assignment

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func targetRemovalFixture(t testing.TB) *sql.DB {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, query := range []string{
		`WITH RECURSIVE seq(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM seq WHERE i<8)
		 INSERT INTO nodes (id,public_name,certificate_fingerprint,state,target_bandwidth_bps,routing_ready,created_at,updated_at)
		 SELECT 'node-'||i,'node-'||i,'cert-'||i,'syncing',0,0,'before','before' FROM seq`,
		`WITH RECURSIVE seq(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM seq WHERE i<20)
		 INSERT INTO projects (id,name,repository,enabled,retain_versions,include_prerelease,download_multiplier,config_hash,updated_at)
		 SELECT 'p'||i,'project-'||i,'owner/repo'||i,1,1,0,1,'hash','before' FROM seq`,
		`INSERT INTO releases (id,project_id,github_release_id,tag_name,prerelease,published_at,selected,created_at)
		 SELECT id||'-old',id,1,'v0',0,'before',0,'before' FROM projects`,
		`INSERT INTO releases (id,project_id,github_release_id,tag_name,prerelease,published_at,selected,created_at)
		 SELECT id||'-new',id,2,'v1',0,'after',1,'before' FROM projects`,
		`WITH RECURSIVE seq(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM seq WHERE i<100)
		 INSERT INTO assets (id,release_id,github_asset_id,file_name,architecture,size_bytes,source_url,digest_sha256,service_state,created_at)
		 SELECT r.id||'-'||i,r.id,i,'file-'||i,'amd64',10,'https://example.invalid','sha256:aa','candidate','before'
		 FROM releases r CROSS JOIN seq`,
		`INSERT INTO node_project_assignments (node_id,project_id,mode,assigned,score,pinned,last_changed_at,updated_at)
		 SELECT n.id,p.id,'auto',1,0,0,'before','before' FROM nodes n CROSS JOIN projects p`,
		`INSERT INTO target_inventory SELECT n.id,a.id,'required','before' FROM nodes n CROSS JOIN assets a`,
		`INSERT INTO node_inventory SELECT n.id,a.id,'sha256:aa',10,'before','verified'
		 FROM nodes n CROSS JOIN assets a JOIN releases r ON r.id=a.release_id WHERE r.selected=1`,
		// Include disabled/unassigned and already removed targets in the comparison.
		`UPDATE projects SET enabled=0 WHERE id='p2'`,
		`UPDATE node_project_assignments SET assigned=0 WHERE project_id='p3'`,
		`UPDATE target_inventory SET desired_state='remove' WHERE asset_id LIKE 'p4-%'`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func targetSnapshot(t *testing.T, tx *sql.Tx) []string {
	t.Helper()
	rows, err := tx.Query(`SELECT node_id||':'||asset_id||':'||desired_state||':'||updated_at
		FROM target_inventory ORDER BY node_id,asset_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestScopedTargetRemovalMatchesLegacy(t *testing.T) {
	for _, project := range []bool{false, true} {
		name := "node"
		if project {
			name = "project"
		}
		t.Run(name, func(t *testing.T) {
			db := targetRemovalFixture(t)
			// Keep one node behind, so retired-release preservation is exercised too.
			mustExec(t, db, `DELETE FROM node_inventory WHERE node_id='node-2' AND asset_id='p1-new-1'`)
			var want []string
			for _, legacy := range []bool{true, false} {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if legacy {
					err = legacyRebuildTargets(tx, project)
				} else if project {
					err = RebuildProjectTargets(context.Background(), tx, "p1", "after")
				} else {
					err = rebuildNodeTargets(context.Background(), tx, "node-1", "after")
				}
				if err != nil {
					_ = tx.Rollback()
					t.Fatal(err)
				}
				got := targetSnapshot(t, tx)
				_ = tx.Rollback()
				if legacy {
					want = got
				} else if !reflect.DeepEqual(got, want) {
					t.Fatal("scoped removal changed target state or timestamp")
				}
			}
		})
	}
}

func BenchmarkTargetRemoval(b *testing.B) {
	for _, project := range []bool{false, true} {
		for _, legacy := range []bool{true, false} {
			name := "node"
			if project {
				name = "project"
			}
			if legacy {
				name += "/legacy"
			} else {
				name += "/scoped"
			}
			b.Run(name, func(b *testing.B) {
				db := targetRemovalFixture(b)
				b.ResetTimer()
				for range b.N {
					tx, err := db.Begin()
					if err != nil {
						b.Fatal(err)
					}
					if legacy {
						err = legacyRebuildTargets(tx, project)
					} else if project {
						err = RebuildProjectTargets(context.Background(), tx, "p1", "after")
					} else {
						err = rebuildNodeTargets(context.Background(), tx, "node-1", "after")
					}
					_ = tx.Rollback()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

var legacyNodeTargetRemovalSQL = `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE desired_state != 'remove'
		AND (node_id, asset_id) IN (
		SELECT ti.node_id, ti.asset_id FROM target_inventory ti
		JOIN assets a ON a.id = ti.asset_id
		JOIN releases r ON r.id = a.release_id
			JOIN projects p ON p.id = r.project_id
			LEFT JOIN node_project_assignments npa ON npa.node_id = ti.node_id
				AND npa.project_id = p.id AND npa.assigned = 1
			WHERE ti.node_id = ? AND (p.enabled = 0
				OR a.service_state NOT IN ('candidate', 'pending', 'active', 'superseded') OR npa.project_id IS NULL
				OR (r.selected = 0 AND ` + nodeHasSelectedProjectAssetsSQL("ti.node_id", "p.id") + `)))`

var legacyProjectTargetRemovalSQL = `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE desired_state != 'remove'
		AND (node_id, asset_id) IN (
		SELECT ti.node_id, ti.asset_id FROM target_inventory ti
			JOIN assets a ON a.id = ti.asset_id
			JOIN releases r ON r.id = a.release_id
			LEFT JOIN node_project_assignments npa ON npa.node_id = ti.node_id
				AND npa.project_id = r.project_id AND npa.assigned = 1
			WHERE r.project_id = ? AND (
				a.service_state NOT IN ('candidate', 'pending', 'active', 'superseded') OR npa.project_id IS NULL
				OR (r.selected = 0 AND ` + nodeHasSelectedProjectAssetsSQL("ti.node_id", "r.project_id") + `)))`

func legacyRebuildTargets(tx *sql.Tx, project bool) error {
	if project {
		if _, err := tx.Exec(legacyProjectTargetInsertSQL, "after", "p1"); err != nil {
			return err
		}
		_, err := tx.Exec(legacyProjectTargetRemovalSQL, "after", "p1")
		return err
	}
	if _, err := tx.Exec(legacyNodeTargetInsertSQL, "node-1", "after", "node-1"); err != nil {
		return err
	}
	_, err := tx.Exec(legacyNodeTargetRemovalSQL, "after", "node-1")
	return err
}

var legacyNodeTargetInsertSQL = `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT ?, a.id, 'required', ?
		FROM node_project_assignments npa
		JOIN releases r ON r.project_id = npa.project_id AND r.selected = 1
		JOIN assets a ON a.release_id = r.id
			WHERE npa.node_id = ? AND npa.assigned = 1
			AND a.service_state IN ('candidate', 'pending', 'active', 'superseded')
			ON CONFLICT(node_id, asset_id) DO UPDATE SET
			desired_state = 'required', updated_at = excluded.updated_at
			WHERE target_inventory.desired_state != 'required'`

var legacyProjectTargetInsertSQL = `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT npa.node_id, a.id, 'required', ?
		FROM node_project_assignments npa
		JOIN releases r ON r.project_id = npa.project_id AND r.selected = 1
		JOIN assets a ON a.release_id = r.id
			WHERE npa.project_id = ? AND npa.assigned = 1
			AND a.service_state IN ('candidate', 'pending', 'active', 'superseded')
			ON CONFLICT(node_id, asset_id) DO UPDATE SET
			desired_state = 'required', updated_at = excluded.updated_at
			WHERE target_inventory.desired_state != 'required'`
