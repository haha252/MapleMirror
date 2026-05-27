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

func testRepo(t *testing.T) (Repository, func()) {
	t.Helper()
	wal := true
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Repository{DB: db}, func() { _ = db.Close() }
}
