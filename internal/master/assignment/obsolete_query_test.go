package assignment

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Keep the previous task SQL as a behavioral/performance reference.
func legacyObsoleteSQL(deleteTask bool, filter string) string {
	query := `UPDATE node_tasks SET state='obsolete', error_message='资产已不在当前目标库存中',
		completed_at=?, updated_at=?, lease_expires_at=NULL WHERE task_type='asset_download'
		AND state IN ('pending','sent','running','retry_wait','failed') AND asset_id IN (
		SELECT a.id FROM assets a JOIN releases r ON r.id=a.release_id
		LEFT JOIN target_inventory ti ON ti.node_id=node_tasks.node_id AND ti.asset_id=a.id
		WHERE ` + filter + ` AND (a.service_state NOT IN ('candidate','pending','active','superseded')
		OR ti.asset_id IS NULL OR ti.desired_state!='required'))`
	if deleteTask {
		query = `UPDATE node_tasks SET state='obsolete', error_message='删除目标已不在当前移除目标中',
			completed_at=?, updated_at=?, lease_expires_at=NULL WHERE task_type='asset_delete'
			AND state IN ('pending','sent','running','retry_wait','failed') AND asset_id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id=a.release_id
			LEFT JOIN target_inventory ti ON ti.node_id=node_tasks.node_id AND ti.asset_id=a.id
			LEFT JOIN node_inventory ni ON ni.node_id=node_tasks.node_id AND ni.asset_id=a.id
			WHERE ` + filter + ` AND (ti.asset_id IS NULL OR ti.desired_state!='remove'
			OR ni.asset_id IS NULL OR ni.state!='verified'))`
	}
	return query
}

func obsoleteQueryFixture(t testing.TB) *sql.DB {
	db := targetRemovalFixture(t)
	states := []string{"pending", "sent", "running", "retry_wait", "failed", "succeeded", "cancelled"}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 140; i++ {
		node := fmt.Sprintf("node-%d", i%2+1)
		asset := fmt.Sprintf("p%d-new-%d", i%20+1, i%100+1)
		for _, kind := range []string{"asset_download", "asset_delete"} {
			if _, err := tx.Exec(`INSERT INTO node_tasks
				(id,node_id,asset_id,task_type,state,request_id,created_at,updated_at,lease_expires_at)
				VALUES (?,?,?,?,?,'req','before','before','lease')`, fmt.Sprintf("%s-%d", kind, i), node, asset, kind, states[i%len(states)]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

func obsoleteSnapshot(t *testing.T, tx *sql.Tx) []string {
	rows, err := tx.Query(`SELECT id,state,COALESCE(error_message,''),COALESCE(completed_at,''),updated_at,COALESCE(lease_expires_at,'') FROM node_tasks ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var row [6]string
		if err := rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4], &row[5]); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join(row[:], "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestObsoleteExistsQueriesMatchLegacyForNodeAndProject(t *testing.T) {
	for _, project := range []bool{false, true} {
		name := "node"
		if project {
			name = "project"
		}
		t.Run(name, func(t *testing.T) {
			db := obsoleteQueryFixture(t)
			var want []string
			for _, legacy := range []bool{true, false} {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if legacy {
					filter, id := "node_tasks.node_id=?", "node-1"
					if project {
						filter, id = "r.project_id=?", "p4"
					}
					for _, del := range []bool{false, true} {
						if _, err = tx.Exec(legacyObsoleteSQL(del, filter), "after", "after", id); err != nil {
							break
						}
					}
				} else if project {
					err = CancelObsoleteProjectTasks(context.Background(), tx, "p4", "after")
				} else {
					err = CancelObsoleteNodeTasks(context.Background(), tx, "node-1", "after")
				}
				if err != nil {
					_ = tx.Rollback()
					t.Fatal(err)
				}
				got := obsoleteSnapshot(t, tx)
				_ = tx.Rollback()
				if legacy {
					want = got
				} else if !reflect.DeepEqual(got, want) {
					t.Fatal("task cancellation changed across queries")
				}
			}
		})
	}
}

func BenchmarkObsoleteNodeTasks(b *testing.B) {
	for _, legacy := range []bool{true, false} {
		name := "exists"
		if legacy {
			name = "legacy"
		}
		b.Run(name, func(b *testing.B) {
			db := obsoleteQueryFixture(b)
			b.ResetTimer()
			for range b.N {
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				if legacy {
					for _, del := range []bool{false, true} {
						if _, err = tx.Exec(legacyObsoleteSQL(del, "node_tasks.node_id=?"), "after", "after", "node-1"); err != nil {
							break
						}
					}
				} else {
					err = CancelObsoleteNodeTasks(context.Background(), tx, "node-1", "after")
				}
				_ = tx.Rollback()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
