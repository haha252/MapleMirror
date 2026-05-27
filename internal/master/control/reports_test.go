package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestReportsDoNotWriteFormalInventory(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r1", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{AssetID: "a1", LocalState: "reported"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var reportCount, formalCount int
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_inventory_reports").Scan(&reportCount)
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_inventory").Scan(&formalCount)
	if reportCount != 1 || formalCount != 0 {
		t.Fatalf("M2 只能保存报告骨架，reports=%d formal=%d", reportCount, formalCount)
	}
}

func TestDisableNodeAuditsAndMasksRouting(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	if err := repo.DisableNode(context.Background(), session.NodeID, "req-disable", "测试禁用"); err != nil {
		t.Fatal(err)
	}
	var state string
	var ready, audits int
	_ = repo.DB.QueryRow("SELECT state, routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&state, &ready)
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE request_id = 'req-disable'").Scan(&audits)
	if state != "disabled" || ready != 0 || audits == 0 {
		t.Fatalf("禁用节点结果错误 state=%s ready=%d audits=%d", state, ready, audits)
	}
}
