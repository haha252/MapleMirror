package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestPublicAssetInventoryMismatchQuarantinesNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-mismatch", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LocalState:   "mismatch",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, certStatus, disconnected string
	var ready, audits int
	_ = repo.DB.QueryRow("SELECT state, routing_ready FROM nodes WHERE id = ?", session.NodeID).Scan(&state, &ready)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	_ = repo.DB.QueryRow(`SELECT COALESCE(disconnected_at, '') FROM node_control_sessions WHERE id = ?`,
		session.ID).Scan(&disconnected)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE operation = 'node.security_quarantine' AND target_id = ?`, session.NodeID).Scan(&audits)
	if state != "disabled" || ready != 0 || certStatus != "revoked" || disconnected == "" || audits != 1 {
		t.Fatalf("节点隔离结果错误 state=%s ready=%d cert=%s disconnected=%q audits=%d",
			state, ready, certStatus, disconnected, audits)
	}
	if repo.runtime().ActiveSession(session.NodeID) {
		t.Fatal("runtime session should be closed after quarantine")
	}
}

func TestNonPublicAssetInventoryMismatchDoesNotQuarantineNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `UPDATE projects SET enabled = 0 WHERE id = 'p1'`)
	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-private-mismatch", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LocalState:   "mismatch",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state, certStatus string
	var audits int
	_ = repo.DB.QueryRow("SELECT state FROM nodes WHERE id = ?", session.NodeID).Scan(&state)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE operation = 'node.security_quarantine'`).Scan(&audits)
	if state == "disabled" || certStatus != "active" || audits != 0 {
		t.Fatalf("非公开资产不应隔离节点 state=%s cert=%s audits=%d", state, certStatus, audits)
	}
}

func TestStalePublicAssetInventoryDoesNotQuarantineNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	oldDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1', ?, 10, '2026-01-01T00:00:00Z', 'stale')`,
		session.NodeID, oldDigest)

	_, err := repo.AcceptInventoryReport(context.Background(), session, 1, protocol.InventoryReport{
		ReportID: "r-stale", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{{
			AssetID: "asset-1", SizeBytes: 10, DigestSHA256: oldDigest, LocalState: "verified",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var nodeState, certStatus, inventoryState string
	var audits, tasks int
	_ = repo.DB.QueryRow("SELECT state FROM nodes WHERE id = ?", session.NodeID).Scan(&nodeState)
	_ = repo.DB.QueryRow("SELECT status FROM node_certificates WHERE id = 'cert-1'").Scan(&certStatus)
	_ = repo.DB.QueryRow(`SELECT state FROM node_inventory
		WHERE node_id = ? AND asset_id = 'asset-1'`, session.NodeID).Scan(&inventoryState)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit_events
		WHERE operation = 'node.security_quarantine'`).Scan(&audits)
	_ = repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND asset_id = 'asset-1' AND task_type = 'asset_download'
		AND state = 'pending'`, session.NodeID).Scan(&tasks)
	if nodeState == "disabled" || certStatus != "active" || inventoryState != "stale" || audits != 0 || tasks != 1 {
		t.Fatalf("stale 库存不应隔离节点 state=%s cert=%s inventory=%s audits=%d tasks=%d",
			nodeState, certStatus, inventoryState, audits, tasks)
	}
}
