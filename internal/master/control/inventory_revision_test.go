package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryReportPersistsRevisionBoundary(t *testing.T) {
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
		t.Fatalf("完整库存应只持久化修订边界，reports=%d formal=%d", reportCount, formalCount)
	}
	latest, err := repo.LatestInventoryReport(context.Background(), session.NodeID)
	if err != nil || latest["revision"] != 1 || latest["complete"] != true {
		t.Fatalf("runtime 库存摘要不符合预期 latest=%v err=%v", latest, err)
	}
}

func TestDuplicateInventoryRevisionAfterReconnectIsIdempotent(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	report := protocol.InventoryReport{
		ReportID: "r-ready", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "reported",
		}},
	}
	if _, err := repo.AcceptInventoryReport(context.Background(), session, 2, report); err != nil {
		t.Fatal(err)
	}
	restarted, err := repo.StartSession(context.Background(), "sha256:aa", "req-reconnect")
	if err != nil {
		t.Fatal(err)
	}
	report.ReportID = "r-ready-retry"
	if _, err := repo.AcceptHeartbeat(context.Background(), restarted, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptInventoryReport(context.Background(), restarted, 2, report); err != nil {
		t.Fatal(err)
	}
	var reportCount, ready, tasks int
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_inventory_reports WHERE node_id = ?", session.NodeID).
		Scan(&reportCount)
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_tasks WHERE node_id = ?", session.NodeID).Scan(&tasks)
	if reportCount != 1 || ready != 1 || tasks != 0 {
		t.Fatalf("重复库存修订应幂等恢复就绪且不生成任务，reports=%d ready=%d tasks=%d",
			reportCount, ready, tasks)
	}
}

func TestStaleInventoryRevisionDoesNotReconcileNewTargets(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	report := protocol.InventoryReport{
		ReportID: "r-ready", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	}
	if _, err := repo.AcceptInventoryReport(context.Background(), session, 2, report); err != nil {
		t.Fatal(err)
	}
	seedSecondAssetTarget(t, repo, session.NodeID)
	report.ReportID = "r-stale"
	if _, err := repo.AcceptInventoryReport(context.Background(), session, 3, report); err != nil {
		t.Fatal(err)
	}
	var staleRows, staleTasks int
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_inventory
		WHERE node_id = ? AND asset_id = 'asset-2'`, session.NodeID).Scan(&staleRows)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-2'`, session.NodeID).Scan(&staleTasks)
	if staleRows != 0 || staleTasks != 0 {
		t.Fatalf("stale revision should not reconcile new target, inventory=%d tasks=%d", staleRows, staleTasks)
	}
	var ready int
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 0 {
		t.Fatalf("stale revision should only recompute current readiness, ready=%d", ready)
	}
	report.ReportID = "r-new"
	report.Revision = 2
	if _, err := repo.AcceptInventoryReport(context.Background(), session, 4, report); err != nil {
		t.Fatal(err)
	}
	var state string
	var repairTasks int
	err := repo.DB.QueryRow(`SELECT state FROM node_inventory
		WHERE node_id = ? AND asset_id = 'asset-2'`, session.NodeID).Scan(&state)
	if err != nil || state != "missing" {
		t.Fatalf("new revision should mark new target missing, state=%q err=%v", state, err)
	}
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-2' AND state = 'pending'`,
		session.NodeID).Scan(&repairTasks)
	if repairTasks != 1 {
		t.Fatalf("new revision should create repair task, tasks=%d", repairTasks)
	}
}
