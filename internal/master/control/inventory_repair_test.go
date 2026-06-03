package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryReportCreatesRepairTaskForMissingTarget(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.DB.Exec(`INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		10, 'old', 'verified')`, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-missing", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	err = repo.DB.QueryRow(`SELECT state FROM node_inventory
		WHERE node_id = ? AND asset_id = 'asset-1'`, session.NodeID).Scan(&state)
	if err != nil || state != "missing" {
		t.Fatalf("missing inventory state got=%q err=%v", state, err)
	}
	var tasks int
	err = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-1' AND state = 'pending'`,
		session.NodeID).Scan(&tasks)
	if err != nil || tasks != 1 {
		t.Fatalf("repair task count=%d err=%v", tasks, err)
	}
}
