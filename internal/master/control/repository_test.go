package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func TestPairingCodeIsOneTime(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	code, err := repo.CreatePairing(ctx, time.Minute, "req-create")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateEnrollment(ctx, code.Code, "节点一", "csr", "sha256:aa", "[]", "req-enroll", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateEnrollment(ctx, code.Code, "节点二", "csr", "sha256:bb", "[]", "req-replay", time.Minute)
	if err == nil {
		t.Fatal("同一个配对码不得重复登记")
	}
}

func TestApproveKeepsRoutingReadyFalse(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	code, _ := repo.CreatePairing(ctx, time.Minute, "req-create")
	enrollment, _ := repo.CreateEnrollment(ctx, code.Code, "节点一", "csr", "sha256:aa", "[]", "req-enroll", time.Minute)
	signed := SignedCertificate{
		NodeID: "node-1", CertificateID: "cert-1", SerialNumber: "1",
		Fingerprint: "sha256:cc", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert", CAChainPEM: "ca",
	}
	if err := repo.ApproveEnrollment(ctx, enrollment.ID, "节点一", "sha256:aa", "req-approve", signed); err != nil {
		t.Fatal(err)
	}
	var ready int
	err := repo.DB.QueryRow("SELECT routing_ready FROM nodes WHERE id = 'node-1'").Scan(&ready)
	if err != nil || ready != 0 {
		t.Fatalf("M2 审批不得使节点可路由，ready=%d err=%v", ready, err)
	}
	delivery, err := repo.CollectCertificate(ctx, enrollment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.NodeID != "node-1" || delivery.CertificatePEM == "" {
		t.Fatal("审批后的证书领取结果不完整")
	}
	if _, err := repo.CollectCertificate(ctx, enrollment.ID); err == nil {
		t.Fatal("证书只能领取一次")
	}
}

func TestApproveEnrollmentSeedsCurrentTargets(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	mustExecControl(t, repo.DB, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES ('p1', '项目', 'owner/repo', 1, 1, 0, 1, 'hash', 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO releases
		(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
		VALUES ('rel-1', 'p1', 1, 'latest', 0, '2026-02-01T00:00:00Z', 1, 'now')`)
	mustExecControl(t, repo.DB, `INSERT INTO assets
		(id, release_id, github_asset_id, file_name, architecture, size_bytes,
		source_url, digest_sha256, service_state, created_at)
		VALUES ('asset-1', 'rel-1', 1, 'ffmpeg.zip', 'amd64', 10,
		'https://example.invalid', 'sha256:aa', 'candidate', 'now')`)
	code, _ := repo.CreatePairing(ctx, time.Minute, "req-create")
	enrollment, _ := repo.CreateEnrollment(ctx, code.Code, "节点一", "csr", "sha256:aa", "[]", "req-enroll", time.Minute)
	signed := SignedCertificate{
		NodeID: "node-1", CertificateID: "cert-1", SerialNumber: "1",
		Fingerprint: "sha256:cc", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert", CAChainPEM: "ca",
	}
	if err := repo.ApproveEnrollment(ctx, enrollment.ID, "节点一", "sha256:aa", "req-approve", signed); err != nil {
		t.Fatal(err)
	}
	assertTableCount(t, repo, "target_inventory",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND desired_state = 'required'", 1)
	assertTableCount(t, repo, "node_tasks",
		"node_id = 'node-1' AND asset_id = 'asset-1' AND state = 'pending'", 1)
}

func TestRotateCertificateKeepsAuthorizedNodeName(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	ctx := context.Background()
	code, _ := repo.CreatePairing(ctx, time.Minute, "req-create")
	enrollment, _ := repo.CreateEnrollment(ctx, code.Code, "授权节点名", "csr", "sha256:aa", "[]", "req-enroll", time.Minute)
	signed := SignedCertificate{
		NodeID: "node-1", CertificateID: "cert-1", SerialNumber: "1",
		Fingerprint: "sha256:cc", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert", CAChainPEM: "ca",
	}
	if err := repo.ApproveEnrollment(ctx, enrollment.ID, "授权节点名", "sha256:aa", "req-approve", signed); err != nil {
		t.Fatal(err)
	}
	rotated := SignedCertificate{
		NodeID: "node-1", CertificateID: "cert-2", SerialNumber: "2",
		Fingerprint: "sha256:dd", NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		CertificatePEM: "cert2", CAChainPEM: "ca",
	}
	if err := repo.RotateCertificate(ctx, "node-1", "req-rotate", "admin", rotated); err != nil {
		t.Fatal(err)
	}
	var publicName string
	if err := repo.DB.QueryRow("SELECT public_name FROM nodes WHERE id = 'node-1'").Scan(&publicName); err != nil {
		t.Fatal(err)
	}
	if publicName != "授权节点名" {
		t.Fatalf("授权后的节点名称不得被后续节点配置改写 got=%q", publicName)
	}
}

func testRepo(t *testing.T) (Repository, func()) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Repository{DB: db, Runtime: NewRuntimeStore()}, func() { _ = db.Close() }
}
