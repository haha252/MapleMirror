package control

import (
	"context"
	"testing"
	"time"
)

func TestApproveEnrollmentDeletesSameNameQuarantinedNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	seedQuarantinedNode(t, repo, "old-node", "节点一")
	code, _ := repo.CreatePairing(ctx, time.Minute, "req-create")
	csr, fp := testEnrollmentCSR(t, "节点一")
	enrollment, _ := repo.CreateEnrollment(ctx, code.Code, "节点一", csr, fp, "[]", "req-enroll", time.Minute)
	signed := SignedCertificate{
		NodeID: "new-node", CertificateID: "new-cert", SerialNumber: "new-1",
		Fingerprint: "sha256:new-cert", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert", CAChainPEM: "ca",
	}

	if err := repo.ApproveEnrollment(ctx, enrollment.ID, "节点一", fp, "req-approve", signed); err != nil {
		t.Fatal(err)
	}

	assertNodeCount(t, repo, "节点一", 1)
	assertTableCount(t, repo, "nodes", "id = 'old-node'", 0)
	assertTableCount(t, repo, "node_certificates", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_control_sessions", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "target_inventory", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_inventory", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_tasks", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "daily_node_traffic_stats", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_project_assignments", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "nodes", "id = 'new-node'", 1)
}

func TestApproveEnrollmentKeepsSameNameManualDisabledNode(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	seedDisabledNode(t, repo, "old-node", "节点一")
	code, _ := repo.CreatePairing(ctx, time.Minute, "req-create")
	csr, fp := testEnrollmentCSR(t, "节点一")
	enrollment, _ := repo.CreateEnrollment(ctx, code.Code, "节点一", csr, fp, "[]", "req-enroll", time.Minute)
	signed := SignedCertificate{
		NodeID: "new-node", CertificateID: "new-cert", SerialNumber: "new-1",
		Fingerprint: "sha256:new-cert", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert", CAChainPEM: "ca",
	}

	if err := repo.ApproveEnrollment(ctx, enrollment.ID, "节点一", fp, "req-approve", signed); err != nil {
		t.Fatal(err)
	}

	assertNodeCount(t, repo, "节点一", 2)
	assertTableCount(t, repo, "nodes", "id = 'old-node'", 1)
	assertTableCount(t, repo, "nodes", "id = 'new-node'", 1)
}

func TestDeleteNodeRemovesRuntimeData(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	seedQuarantinedNode(t, repo, "old-node", "节点一")

	if err := repo.DeleteNode(ctx, "old-node", "req-delete", "admin"); err != nil {
		t.Fatal(err)
	}

	assertTableCount(t, repo, "nodes", "id = 'old-node'", 0)
	assertTableCount(t, repo, "node_certificates", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_control_sessions", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "target_inventory", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_inventory", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_tasks", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "daily_node_traffic_stats", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "node_project_assignments", "node_id = 'old-node'", 0)
	assertTableCount(t, repo, "admin_audit_events", "operation = 'node.delete'", 1)
}

func seedQuarantinedNode(t *testing.T, repo Repository, nodeID, name string) {
	t.Helper()
	seedDisabledNode(t, repo, nodeID, name)
	execApprovalCleanup(t, repo, `INSERT INTO admin_audit_events
		(id, operation, target_type, target_id, result, request_id, created_at,
		admin_identity, details_summary)
		VALUES ('audit-quarantine', 'node.security_quarantine', 'node', ?, 'success',
		'req-quarantine', 'now', '', '公开资产库存校验不一致')`, nodeID)
	seedAssetTarget(t, repo, nodeID)
	execApprovalCleanup(t, repo, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1', 'sha256:bb', 10, 'now', 'mismatch')`, nodeID)
	execApprovalCleanup(t, repo, `INSERT INTO node_tasks
		(id, node_id, task_type, asset_id, state, request_id, created_at, updated_at)
		VALUES ('task-old', ?, 'asset_download', 'asset-1', 'pending', 'req-task', 'now', 'now')`, nodeID)
	execApprovalCleanup(t, repo, `INSERT INTO daily_node_traffic_stats
		(stat_day, node_id, sent_bytes, updated_at) VALUES ('2026-06-05', ?, 10, 'now')`, nodeID)
	execApprovalCleanup(t, repo, `INSERT INTO node_project_assignments
		(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
		VALUES (?, 'p1', 'manual', 1, 10, 0, 'now', 'now')`, nodeID)
}

func seedDisabledNode(t *testing.T, repo Repository, nodeID, name string) {
	t.Helper()
	execApprovalCleanup(t, repo, `INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		routing_ready, created_at, updated_at)
		VALUES (?, ?, 'sha256:old', 'disabled', 0, 0, 'now', 'now')`, nodeID, name)
	execApprovalCleanup(t, repo, `INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after, status,
		issued_request_id, revoked_at, created_at)
		VALUES ('cert-old', ?, 'old-1', 'sha256:old-cert', 'now', '2999-01-01T00:00:00Z',
		'revoked', 'req-old', 'now', 'now')`, nodeID)
	execApprovalCleanup(t, repo, `INSERT INTO node_control_sessions
		(id, node_id, certificate_id, request_id, connected_at, last_message_sequence,
		disconnected_at, close_reason)
		VALUES ('session-old', ?, 'cert-old', 'req-old', 'now', 1, 'now', '公开资产库存校验不一致')`, nodeID)
}

func assertNodeCount(t *testing.T, repo Repository, name string, want int) {
	t.Helper()
	assertTableCount(t, repo, "nodes", "public_name = '"+name+"'", want)
}

func assertTableCount(t *testing.T, repo Repository, table, where string, want int) {
	t.Helper()
	var got int
	if err := repo.DB.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + where).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count mismatch got=%d want=%d where=%s", table, got, want, where)
	}
}

func execApprovalCleanup(t *testing.T, repo Repository, stmt string, args ...any) {
	t.Helper()
	if _, err := repo.DB.Exec(stmt, args...); err != nil {
		t.Fatal(err)
	}
}
