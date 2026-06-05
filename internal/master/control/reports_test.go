package control

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestReportsKeepInventorySummaryInRuntimeOnly(t *testing.T) {
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
	if reportCount != 0 || formalCount != 0 {
		t.Fatalf("库存摘要应只留在内存，reports=%d formal=%d", reportCount, formalCount)
	}
	latest, err := repo.LatestInventoryReport(context.Background(), session.NodeID)
	if err != nil || latest["revision"] != 1 || latest["complete"] != true {
		t.Fatalf("runtime 库存摘要不符合预期 latest=%v err=%v", latest, err)
	}
}

func TestCompleteInventoryReportCanMarkNodeReady(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-ready", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "reported",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	var state string
	_ = repo.DB.QueryRow("SELECT routing_ready, state FROM nodes WHERE id = ?", session.NodeID).Scan(&ready, &state)
	if ready != 1 || state != "online" {
		t.Fatalf("完整库存对账通过后只应更新同步就绪，ready=%d state=%s", ready, state)
	}
}

func TestCompleteInventoryWithoutHeartbeatDoesNotMarkReady(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-no-heartbeat", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "reported",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 0 {
		t.Fatalf("无心跳节点不得完成全量同步就绪，ready=%d", ready)
	}
}

func TestReconnectResetsReadyUntilCompleteInventoryReportArrives(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.AcceptInventoryReport(context.Background(), session, 2, protocol.InventoryReport{
		ReportID: "r-ready", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "reported",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ready int
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 1 {
		t.Fatalf("expected ready before reconnect, got %d", ready)
	}
	restarted, err := repo.StartSession(context.Background(), "sha256:aa", "req-reconnect")
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 0 {
		t.Fatalf("expected ready reset on reconnect, got %d", ready)
	}
	if _, err := repo.AcceptHeartbeat(context.Background(), restarted, 1, protocol.Heartbeat{Status: "syncing"}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.AcceptInventoryReport(context.Background(), restarted, 2, protocol.InventoryReport{
		ReportID: "r-reconnect", Revision: 2, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "reported",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if ready != 1 {
		t.Fatalf("expected ready restored after complete report, got %d", ready)
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
	var reportCount, ready int
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_inventory_reports WHERE node_id = ?", session.NodeID).
		Scan(&reportCount)
	_ = repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&ready)
	if reportCount != 0 || ready != 1 {
		t.Fatalf("重复库存修订应幂等并恢复就绪，reports=%d ready=%d", reportCount, ready)
	}
}

func TestPressureReportReplayHasNoDuplicateSideEffect(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	report := protocol.PressureReport{
		ReportID: "p1", SampledAt: time.Now(), SampleWindowSeconds: 10,
		TargetBandwidthBPS: 100, ActualBandwidthBPS: 50, FreeBytes: 1,
	}
	if _, err := repo.AcceptPressureReport(context.Background(), session, 1, report); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AcceptPressureReport(context.Background(), session, 1, report); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = repo.DB.QueryRow("SELECT COUNT(*) FROM node_pressure_reports").Scan(&count)
	if count != 0 {
		t.Fatalf("压力报告不应写入 SQLite 历史表，count=%d", count)
	}
	latest, err := repo.LatestPressureReport(context.Background(), session.NodeID)
	if err != nil || latest["pressure_ratio"] != 0.5 {
		t.Fatalf("runtime 压力报告不符合预期 latest=%v err=%v", latest, err)
	}
}

func TestRuntimeLatestReportsAreEmptyAfterRuntimeReset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	_, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Status: "syncing", FreeBytes: 100, PublicDownloadBaseURL: "https://node-1.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	restarted := Repository{DB: repo.DB, Runtime: NewRuntimeStore()}
	if _, err := restarted.LatestHeartbeat(context.Background(), session.NodeID); err != sql.ErrNoRows {
		t.Fatalf("重启式 runtime 不应返回旧心跳样本 err=%v", err)
	}
	var heartbeat string
	if err := restarted.DB.QueryRow(`SELECT COALESCE(last_heartbeat_at, '')
		FROM nodes WHERE id = ?`, session.NodeID).Scan(&heartbeat); err != nil || heartbeat == "" {
		t.Fatalf("节点路由所需最近心跳字段应保留在 SQLite heartbeat=%q err=%v", heartbeat, err)
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
