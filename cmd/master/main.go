package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/admin"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/health"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/master/public"
	"mirror-server/internal/requestid"
	"mirror-server/internal/storage"
)

var version = "寮€鍙戠増"

func main() {
	path := flag.String("config", "config.yaml", "主节点配置文件路径")
	projectsPath := flag.String("projects", "projects.yaml", "项目清单配置文件路径")
	quotaPath := flag.String("quota", "quota.yaml", "额度配置文件路径")
	flag.Parse()

	var warnings [][2]string
	warn := func(field, value string) { warnings = append(warnings, [2]string{field, value}) }
	var created bool

	cfg, err := config.LoadMaster(*path, warn)
	if handleLoad(err, "主节点配置", &created) {
		os.Exit(1)
	}
	projects, err := config.LoadProjects(*projectsPath, warn)
	if handleLoad(err, "项目清单", &created) {
		os.Exit(1)
	}
	quota, err := config.LoadQuota(*quotaPath, warn)
	if handleLoad(err, "额度配置", &created) {
		os.Exit(1)
	}

	if created {
		cfg, err = config.LoadMaster(*path, warn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "主节点配置加载失败：%v\n", err)
			os.Exit(1)
		}
		if err := bootstrap.MasterFirstRun(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "主节点首次初始化未完成：%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "已生成主节点所需示例配置和安全材料，请确认项目配置后重新启动。")
		return
	}
	if bootstrap.MasterNeedsMaterials(cfg) {
		if err := bootstrap.MasterFirstRun(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "主节点首次初始化未完成：%v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "主节点安全材料已生成，请重新启动。")
		return
	}

	location, _ := time.LoadLocation(cfg.Stats.Timezone)
	logger, err := logging.New("master", cfg.Logging, location, os.Stdout)
	if err != nil {
		logging.StartupError("主节点", err)
		os.Exit(1)
	}
	defer logger.Close()
	for _, item := range warnings {
		logger.ConfigWarning(item[0], item[1])
	}

	database, err := storage.OpenMaster(cfg.Database)
	if err != nil {
		logger.Error(context.Background(), "数据库初始化失败，主节点无法启动", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()
	logger.Info(context.Background(), "主节点数据库迁移已完成")

	repo := mastercontrol.Repository{DB: database, Logger: logger}
	syncService := startMirrorSync(cfg, projects, database, logger)
	startControlServices(cfg, repo, logger)
	startAdminService(cfg, repo, syncService, logger)
	startConsolePairing(cfg, repo, logger)

	publicHandler, err := publicHandler(cfg, quota, location, database, logger)
	if err != nil {
		logger.Error(context.Background(), "公共下载链路初始化失败", slog.String("error", err.Error()))
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Ready: func() bool { return true }, Version: version,
	}, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	mux.Handle("/", requestid.Middleware(publicHandler, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	runServer(cfg.Server.PublicListen, mux, logger)
}

func handleLoad(err error, name string, created *bool) bool {
	if errors.Is(err, config.ErrExampleCreated) {
		*created = true
		return false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s加载失败：%v\n", name, err)
		return true
	}
	return false
}

func startMirrorSync(cfg config.Master, projects config.Projects, db *sql.DB, logger *logging.Logger) mirrorsync.Service {
	interval, _ := time.ParseDuration(cfg.Scan.Interval)
	token := ""
	if cfg.Scan.GitHubTokenEnv != "" {
		token = os.Getenv(cfg.Scan.GitHubTokenEnv)
	}
	store := mirrorsync.Store{DB: db}
	service := mirrorsync.Service{
		Scanner: mirrorsync.Scanner{
			Store: store, GitHub: mirrorsync.HTTPGitHubClient{Token: token}, Logger: logger,
		},
		Projects: projects, Interval: interval, Logger: logger,
	}
	ctx := context.Background()
	go service.Run(ctx)
	logger.Info(ctx, "Release 扫描调度已启动", slog.String("interval", cfg.Scan.Interval))
	return service
}

func publicHandler(cfg config.Master, quota config.Quota, loc *time.Location, db *sql.DB, logger *logging.Logger) (http.Handler, error) {
	signer, err := downloadtoken.NewSignerFromPrivateFile(cfg.DownloadToken.SigningPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	altchaTTL, _ := time.ParseDuration(cfg.ALTCHA.ChallengeTTL)
	apiTTL, _ := time.ParseDuration(cfg.APIPoW.ChallengeTTL)
	tokenTTL, _ := time.ParseDuration(cfg.DownloadToken.TTL)
	logger.Info(context.Background(), "公共下载链路已启用")
	server := public.New(db, signer, altchaTTL, apiTTL, tokenTTL,
		cfg.APIPoW.LeadingZeroBits, quota, loc, cfg.Proxy.TrustedCIDRs, logger)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_ = server.Store.SampleNodeAvailability(context.Background())
		}
	}()
	return server.Handler(), nil
}

func startAdminService(cfg config.Master, repo mastercontrol.Repository, syncService mirrorsync.Service, logger *logging.Logger) {
	if cfg.Admin.TLS.CertFile == "" || cfg.Admin.TLS.KeyFile == "" {
		logger.Warn(context.Background(), "管理 API TLS 材料未配置，管理服务未启动")
		return
	}
	auth, err := admin.NewAuth(cfg.Admin)
	if err != nil {
		logger.Error(context.Background(), "管理 API 鉴权初始化失败", slog.String("error", err.Error()))
		return
	}
	if cfg.Node.TLS.SigningCACertFile == "" || cfg.Node.TLS.SigningCAKeyFile == "" {
		logger.Error(context.Background(), "节点证书签发 CA 未配置，管理服务未启动")
		return
	}
	loaded, err := mastercontrol.LoadCertificateSigner(
		cfg.Node.TLS.SigningCACertFile, cfg.Node.TLS.SigningCAKeyFile, 365*24*time.Hour)
	if err != nil {
		logger.Error(context.Background(), "节点证书签发器初始化失败", slog.String("error", err.Error()))
		return
	}
	handler := requestid.Middleware(admin.Server{
		Auth: auth, Repo: repo, Signer: loaded.Sign,
		Sync: syncService, SyncStore: syncService.Scanner.Store, Logger: logger,
	}.Handler(), cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader)
	tlsCfg, err := controltls.AdminServer(cfg.Admin.TLS.CertFile, cfg.Admin.TLS.KeyFile, cfg.Admin.TLS.ClientCAFile)
	if err != nil {
		logger.Error(context.Background(), "管理 API TLS 初始化失败", slog.String("error", err.Error()))
		return
	}
	server := &http.Server{
		Addr: cfg.Server.ManagementListen, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsCfg,
	}
	go func() {
		logger.Info(context.Background(), "管理 API 已启动", slog.String("listen", cfg.Server.ManagementListen))
		if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(context.Background(), "管理 API 异常退出", slog.String("error", err.Error()))
		}
	}()
}

func startControlServices(cfg config.Master, repo mastercontrol.Repository, logger *logging.Logger) {
	timeout, _ := time.ParseDuration(cfg.Node.HeartbeatTimeout)
	interval, _ := time.ParseDuration(cfg.Node.HeartbeatInterval)
	if cfg.Server.ControlListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.ControlServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, cfg.Node.TLS.ClientCAFile)
		startTLSListener(cfg.Server.ControlListen, tlsCfg, err, logger, mastercontrol.ControlServer{
			Repo: repo, HeartbeatInterval: interval, HeartbeatTimeout: timeout, Logger: logger,
		}.Handle)
	}
	if cfg.Server.EnrollmentListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.EnrollmentServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, "")
		enrollTimeout, _ := time.ParseDuration(cfg.Node.EnrollmentTimeout)
		publicKey, keyErr := bootstrap.PublicKeyPEM(cfg.DownloadToken.VerifyPublicKeyFile)
		if keyErr != nil {
			err = keyErr
		}
		caData, caErr := os.ReadFile(cfg.Node.TLS.CAFile)
		if caErr != nil {
			err = caErr
		}
		startTLSListener(cfg.Server.EnrollmentListen, tlsCfg, err, logger, mastercontrol.EnrollmentServer{
			Repo: repo, EnrollmentTimeout: enrollTimeout, DownloadTokenPublicKeyPEM: publicKey,
			MasterCAPEM: string(caData),
		}.Handle)
	}
	startHeartbeatSweep(repo, timeout, logger)
}
