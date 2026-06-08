package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/files"
	"mirror-server/internal/node/health"
	nodeprobe "mirror-server/internal/node/probe"
	"mirror-server/internal/node/syncer"
	"mirror-server/internal/requestid"
	"mirror-server/internal/storage"
)

var version = "开发版"

func main() {
	path := flag.String("config", "node.yaml", "下载节点配置文件路径")
	flag.Parse()
	var warnings [][2]string
	cfg, created, err := loadNodeConfig(*path, func(field, value string) {
		warnings = append(warnings, [2]string{field, value})
	})
	if created {
		fmt.Fprintln(os.Stderr, "已生成下载节点示例配置，请确认安全字段后重新启动。")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "下载节点配置加载失败：%v\n", err)
		os.Exit(1)
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	logger, err := logging.New("node", cfg.Logging, location, os.Stdout)
	if err != nil {
		logging.StartupError("下载节点", err)
		os.Exit(1)
	}
	defer logger.Close()
	for _, item := range warnings {
		logger.ConfigWarning(item[0], item[1])
	}
	database, err := storage.OpenNode(cfg.Storage.StateDB)
	if err != nil {
		logger.Error(context.Background(), "节点状态库初始化失败，下载节点无法就绪", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()
	logger.Info(context.Background(), "下载节点本地状态库迁移已完成")
	if tempDir, removed, err := syncer.CleanTempDirectory(cfg.Storage.Directory, cfg.Storage.TempDirectory); err != nil {
		logger.Error(context.Background(), "下载节点临时目录启动清理失败",
			slog.String("error", err.Error()),
			slog.String("temp_directory", tempDir))
		os.Exit(1)
	} else {
		logger.Info(context.Background(), "下载节点临时目录启动清理已完成",
			slog.Int("removed_entries", removed),
			slog.String("temp_directory", tempDir))
	}
	if err := interactiveEnrollIfNeeded(*path, &cfg, database); err != nil {
		logger.Error(context.Background(), "下载节点首次交互登记失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	startEnrollmentClient(cfg, database, logger)
	probeStore := publicProbeStore(cfg, database, logger)
	startControlClient(cfg, database, logger, probeStore)
	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Version: version,
	}, "X-Request-ID", "X-Request-ID"))
	if handler := fileHandler(cfg, database, logger, probeStore); handler != nil {
		mux.Handle("/downloads/", requestid.Middleware(handler, "X-Request-ID", "X-Request-ID"))
		mux.Handle("/", requestid.Middleware(handler, "X-Request-ID", "X-Request-ID"))
	}
	runServer(cfg.Server.Listen, mux, logger)
}

func fileHandler(cfg config.Node, db *sql.DB, logger *logging.Logger,
	probes *nodeprobe.Store) http.Handler {
	signer, err := downloadtoken.NewVerifierFromPublicFile(cfg.Download.VerifyPublicKeyFile)
	if err != nil {
		logger.Error(context.Background(), "下载令牌验证公钥加载失败，文件服务未启动", slog.String("error", err.Error()))
		return nil
	}
	nodeID, err := (nodecontrol.IdentityStore{DB: db}).NodeID()
	if err != nil {
		logger.Warn(context.Background(), "节点身份尚未登记，文件服务未启动")
		return nil
	}
	return &files.Handler{DB: db, Storage: cfg.Storage.Directory, NodeID: nodeID,
		Signer: signer, TrustedCIDRs: cfg.Proxy.TrustedCIDRs, Logger: logger,
		ProbeStore: probes}
}

func publicProbeStore(cfg config.Node, db *sql.DB, logger *logging.Logger) *nodeprobe.Store {
	nodeID, err := (nodecontrol.IdentityStore{DB: db}).NodeID()
	if err != nil {
		return nil
	}
	store, err := nodeprobe.NewStore(nodeID, cfg.TLS.KeyFile)
	if err != nil {
		logger.Warn(context.Background(), "节点公网探测签名材料加载失败",
			slog.String("error", err.Error()))
		return nil
	}
	return store
}

func startEnrollmentClient(cfg config.Node, db *sql.DB, logger *logging.Logger) {
	if cfg.Master.EnrollmentAddress == "" || cfg.Pairing.CodeFile == "" {
		return
	}
	if _, err := os.Stat(cfg.TLS.CertFile); err == nil {
		return
	}
	address, err := url.Parse(cfg.Master.EnrollmentAddress)
	if err != nil {
		logger.Error(context.Background(), "主节点登记地址无效", slog.String("error", err.Error()))
		return
	}
	tlsCfg, err := controltls.NodeClient(cfg.TLS.CAFile, "", "", cfg.TLS.ServerName)
	if err != nil {
		logger.Error(context.Background(), "节点登记 TLS 初始化失败", slog.String("error", err.Error()))
		return
	}
	enroller := nodecontrol.Enroller{
		NodeName: cfg.Node.Name, Address: address.Host, TLSConfig: tlsCfg,
		CodeFile: cfg.Pairing.CodeFile, CredentialFile: cfg.Pairing.CredentialFile,
		TokenPublicKeyFile: cfg.Download.VerifyPublicKeyFile,
		Identity: nodecontrol.IdentityStore{DB: db, CertFile: cfg.TLS.CertFile,
			KeyFile: cfg.TLS.KeyFile, CAFile: cfg.TLS.CAFile},
	}
	go func() {
		if err := enroller.RunOnce(); err != nil {
			logger.Warn(context.Background(), "节点首次登记未完成", slog.String("error", err.Error()))
		}
	}()
}

func interactiveEnrollIfNeeded(path string, cfg *config.Node, db *sql.DB) error {
	if _, err := (nodecontrol.IdentityStore{DB: db}).NodeID(); err == nil {
		return nil
	}
	answers, err := bootstrap.NodeFirstRun(*cfg)
	if err != nil {
		return err
	}
	cfg.Node.Name = answers.Name
	cfg.Master.ControlAddress = answers.ControlAddress
	cfg.Master.EnrollmentAddress = answers.EnrollmentAddress
	cfg.TLS.ServerName = answers.ServerName
	if err := config.SaveNodeFirstRun(path, *cfg); err != nil {
		return err
	}
	if err := bootstrap.WritePairingCode(cfg.Pairing.CodeFile, answers.PairingCode); err != nil {
		return err
	}
	address, err := url.Parse(cfg.Master.EnrollmentAddress)
	if err != nil {
		return err
	}
	enroller := nodecontrol.Enroller{
		NodeName: cfg.Node.Name, Address: address.Host, TLSConfig: answers.TLSConfig,
		CodeFile: cfg.Pairing.CodeFile, CredentialFile: cfg.Pairing.CredentialFile,
		TokenPublicKeyFile: cfg.Download.VerifyPublicKeyFile,
		Identity: nodecontrol.IdentityStore{DB: db, CertFile: cfg.TLS.CertFile,
			KeyFile: cfg.TLS.KeyFile, CAFile: cfg.TLS.CAFile},
	}
	return enroller.RunUntilComplete(15 * time.Minute)
}

func startControlClient(cfg config.Node, db *sql.DB, logger *logging.Logger,
	probes *nodeprobe.Store) {
	if cfg.TLS.CAFile == "" || cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
		logger.Warn(context.Background(), "节点控制证书材料未配置，控制连接未启动")
		return
	}
	address, err := url.Parse(cfg.Master.ControlAddress)
	if err != nil {
		logger.Error(context.Background(), "主节点控制地址无效", slog.String("error", err.Error()))
		return
	}
	supervisor := controlSupervisor{
		cfg: cfg, db: db, logger: logger, address: address.Host,
		executor: syncer.Executor{DB: db, Storage: cfg.Storage.Directory,
			TempDir: cfg.Storage.TempDirectory, Logger: logger,
			Probe: syncer.NewSourceProbe(nil), BandwidthLimitBPS: cfg.Sync.BandwidthLimitBPS},
		limiter:   nodecontrol.NewTaskLimiter(cfg.Sync.MaxWorkers),
		bandwidth: nodecontrol.NewNetworkBandwidthSampler(),
		probes:    probes,
	}
	go supervisor.run()
	logger.Info(context.Background(), "节点主动控制连接已启动", slog.String("master", address.Host))
}

func runServer(address string, handler http.Handler, logger *logging.Logger) {
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info(context.Background(), "下载节点健康服务已启动", slog.String("listen", address))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error(context.Background(), "下载节点健康服务异常退出", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
