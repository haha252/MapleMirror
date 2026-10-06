package assignment

import (
	"context"
	"testing"
)

func TestGeneratedTasksAndNewTargetsInvalidateReadinessInTransaction(t *testing.T) {
	db := testDB(t)
	seedAssignmentNode(t, db, 0)
	seedProject(t, db, "p1", "Project", 0)
	mustExec(t, db, `UPDATE nodes SET state='online',last_heartbeat_at='now',routing_ready=1 WHERE id='node-1'`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := ReconcileNode(context.Background(), tx, "node-1", "after"); err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := tx.QueryRow(`SELECT routing_ready FROM nodes WHERE id='node-1'`).Scan(&ready); err != nil || ready != 0 {
		t.Fatalf("new targets ready=%d err=%v", ready, err)
	}
	if _, err := tx.Exec(`INSERT INTO node_inventory(node_id,asset_id,local_digest_sha256,size_bytes,verified_at,state) VALUES('node-1','asset-p1','sha256',64,'now','verified')`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileReadiness(context.Background(), tx, "node-1", "verified"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT routing_ready FROM nodes WHERE id='node-1'`).Scan(&ready); err != nil || ready != 1 {
		t.Fatalf("verified ready=%d err=%v", ready, err)
	}
	// A digest change needs work even though the old inventory was verified.
	if _, err := tx.Exec(`UPDATE assets SET digest_sha256='new-sha' WHERE id='asset-p1'`); err != nil {
		t.Fatal(err)
	}
	if generated, err := GenerateNodeTasks(context.Background(), tx, "node-1", "changed"); err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v", generated, err)
	}
	if err := tx.QueryRow(`SELECT routing_ready FROM nodes WHERE id='node-1'`).Scan(&ready); err != nil || ready != 0 {
		t.Fatalf("pending task ready=%d err=%v", ready, err)
	}
}
