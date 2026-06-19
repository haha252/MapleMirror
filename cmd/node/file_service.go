package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/files"
	nodeprobe "mirror-server/internal/node/probe"
	"mirror-server/internal/node/trafficlimit"
)

func fileHandler(cfg config.Node, db *sql.DB, logger *logging.Logger,
	probes *nodeprobe.Store, limiter *trafficlimit.Limiter) http.Handler {
	identity := nodecontrol.IdentityStore{DB: db, CertFile: cfg.TLS.CertFile,
		KeyFile: cfg.TLS.KeyFile, CAFile: cfg.TLS.CAFile}
	signer, err := identity.DownloadTokenVerifier()
	if err != nil {
		signer, err = downloadtoken.NewVerifierFromPublicFile(cfg.Download.VerifyPublicKeyFile)
		if err != nil {
			logger.Error(context.Background(), "下载令牌验证公钥加载失败，文件服务未启动",
				slog.String("error", err.Error()))
			return nil
		}
		if data, readErr := os.ReadFile(cfg.Download.VerifyPublicKeyFile); readErr == nil {
			_ = identity.SaveDownloadTokenPublicKey(data)
		}
	}
	nodeID, err := identity.NodeID()
	if err != nil {
		logger.Warn(context.Background(), "节点身份尚未登记，文件服务未启动")
		return nil
	}
	return &files.Handler{DB: db, Storage: cfg.Storage.Directory, NodeID: nodeID,
		Signer: signer, TrustedCIDRs: cfg.Proxy.TrustedCIDRs, Logger: logger,
		TrafficLimiter: limiter, ProbeStore: probes}
}
