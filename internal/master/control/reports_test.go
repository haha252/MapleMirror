package control

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

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

func TestReconnectPreservesReadyWhenInventoryAlreadyMatches(t *testing.T) {
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
	if ready != 1 {
		t.Fatalf("expected ready preserved on reconnect, got %d", ready)
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

func TestCompleteInventoryReportRemovesVerifiedNonTargetAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-remove-non-target", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var desiredState string
	if err := repo.DB.QueryRow(`SELECT desired_state FROM target_inventory
		WHERE node_id = ? AND asset_id = 'asset-new'`, session.NodeID).Scan(&desiredState); err != nil {
		t.Fatal(err)
	}
	if desiredState != "remove" {
		t.Fatalf("desired_state=%s want remove", desiredState)
	}
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 1)
	if !result.SyncTasksChanged {
		t.Fatal("non-target verified asset should trigger delete tasks changed")
	}
}

func TestCompleteInventoryReportCleansHistoricalVerifiedResidue(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-clean-residue", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND desired_state = 'remove'", 1)
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 1)
}

func TestCompleteInventoryReportDoesNotDuplicateSucceededDeleteTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at, completed_at)
		VALUES ('delete-done', ?, 'asset_delete', 'asset-new', 'succeeded',
		'req-done', 'old', 'old', 'old')`, session.NodeID)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-no-dup-delete", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete'", 1)
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete' AND state = 'pending'", 0)
	if result.SyncTasksChanged {
		t.Fatal("existing succeeded delete task should not count as new task change")
	}
}

func TestCompleteInventoryReportRemovedNonTargetAssetDoesNotCreateDeleteTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedUnassignedPendingAsset(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-new', 'sha256:new', 20, 'old', 'verified')`, session.NodeID)

	result, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-removed-non-target", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID:      "asset-new",
			DigestSHA256: "sha256:new",
			SizeBytes:    20,
			LocalState:   "removed",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var state, digest string
	var size int64
	if err := repo.DB.QueryRow(`SELECT state, local_digest_sha256, size_bytes
		FROM node_inventory WHERE node_id = ? AND asset_id = 'asset-new'`,
		session.NodeID).Scan(&state, &digest, &size); err != nil {
		t.Fatal(err)
	}
	if state != "removed" || digest != "" || size != 0 {
		t.Fatalf("removed non-target inventory got state=%s digest=%q size=%d",
			state, digest, size)
	}
	var targetCount int
	var desiredState string
	if err := repo.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(desired_state), '')
		FROM target_inventory WHERE node_id = ? AND asset_id = 'asset-new'`,
		session.NodeID).Scan(&targetCount, &desiredState); err != nil {
		t.Fatal(err)
	}
	if targetCount != 0 {
		t.Fatalf("unexpected target_inventory for removed non-target asset count=%d desired_state=%q",
			targetCount, desiredState)
	}
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-new' AND task_type = 'asset_delete'", 0)
	if result.SyncTasksChanged {
		t.Fatal("removed non-target asset should not create delete task")
	}
}

func TestCompleteInventoryReportKeepsRequiredAssetRequired(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-required-stays-required", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LocalState:   "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'required'", 1)
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'remove'", 0)
}
