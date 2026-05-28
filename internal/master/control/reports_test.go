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

func TestCompleteInventoryReportCanMarkNodeReady(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
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
		t.Fatalf("完整库存对账通过后应允许同步就绪，ready=%d", ready)
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
	if count != 1 {
		t.Fatalf("重复压力报告不应产生副作用，count=%d", count)
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

func seedAssetTarget(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	_, err := repo.DB.Exec(`INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'v1', 0, 'now', 1, 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'app.zip', 'amd64', 10, 'https://example.invalid',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'candidate', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.DB.Exec(`INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		VALUES (?, 'asset-1', 'required', 'now')`, nodeID)
	if err != nil {
		t.Fatal(err)
	}
}
