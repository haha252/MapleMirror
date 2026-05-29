package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
)

func startConsolePairing(cfg config.Master, repo mastercontrol.Repository, logger *logging.Logger) {
	if !bootstrap.Interactive() {
		return
	}
	signer, err := mastercontrol.LoadCertificateSigner(
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile, 365*24*time.Hour)
	if err != nil {
		logger.Warn(context.Background(), "本地控制台配对审批未启动", slog.String("error", err.Error()))
		return
	}
	go consolePairingLoop(cfg, repo, signer, logger)
}

func consolePairingLoop(cfg config.Master, repo mastercontrol.Repository,
	signer mastercontrol.CertificateSigner, logger *logging.Logger) {
	reader := bufio.NewReader(os.Stdin)
	ttl, _ := time.ParseDuration(cfg.Node.PairingCodeTTL)
	for {
		item, ok := nextPending(repo)
		if ok {
			handlePendingEnrollment(reader, repo, signer, item)
			continue
		}
		fmt.Print("本地配对控制台：按 Enter 创建一次性配对码，输入 skip 暂不创建：")
		text, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(text)) == "skip" {
			time.Sleep(10 * time.Second)
			continue
		}
		code, err := repo.CreatePairing(context.Background(), ttl, "local-console")
		if err != nil {
			logger.Warn(context.Background(), "本地配对码创建失败", slog.String("error", err.Error()))
			continue
		}
		fmt.Printf("一次性配对码：%s\n有效期至：%s\n", code.Code,
			code.ExpiresAt.Local().Format(time.RFC3339))
	}
}

func nextPending(repo mastercontrol.Repository) (mastercontrol.Enrollment, bool) {
	items, err := repo.PendingEnrollments(context.Background())
	if err != nil || len(items) == 0 {
		return mastercontrol.Enrollment{}, false
	}
	return items[0], true
}

func handlePendingEnrollment(reader *bufio.Reader, repo mastercontrol.Repository,
	signer mastercontrol.CertificateSigner, item mastercontrol.Enrollment) {
	fmt.Println("发现待审批节点登记：")
	fmt.Println("登记 ID：", item.ID)
	fmt.Println("节点名称：", item.PublicName)
	fmt.Println("公钥指纹：", item.Fingerprint)
	fmt.Println("过期时间：", item.ExpiresAt.Local().Format(time.RFC3339))
	fmt.Print("按 Enter 批准，输入 reject 拒绝：")
	text, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(text)) == "reject" {
		_ = repo.RejectEnrollment(context.Background(), item.ID, "local-console",
			"本地控制台拒绝", "local-console")
		return
	}
	csrPEM, detail, err := repo.CSRForEnrollment(context.Background(), item.ID)
	if err != nil {
		fmt.Println("读取登记请求失败：", err)
		return
	}
	csr, err := controltls.ParseCSR([]byte(csrPEM))
	if err != nil {
		fmt.Println("CSR 不合法：", err)
		return
	}
	signed, err := signer.Sign(csr)
	if err != nil {
		fmt.Println("签发节点证书失败：", err)
		return
	}
	err = repo.ApproveEnrollment(context.Background(), item.ID, detail.PublicName,
		detail.Fingerprint, "local-console", signed)
	if err != nil {
		fmt.Println("批准登记失败：", err)
		return
	}
	fmt.Println("登记请求已批准，节点 ID：", signed.NodeID)
}
