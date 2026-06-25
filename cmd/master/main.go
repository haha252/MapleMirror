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
	"mirror-server/internal/master/accountingarchive"
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
	noticesPath := flag.String("notices", "notices.yaml", "公告配置文件路径")
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
	notices, err := config.LoadNotices(*noticesPath, warn)
	if handleLoad(err, "公告配置", &created) {
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
		fmt.Fprintln(os.Stderr, "已生成主节点所需示例配置和安全材料，请确认项目、额度和公告配置后重新启动。")
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

	database, err := storage.OpenMaster(cfg.Database, storage.WithVersionLogger(logger.Info))
	if err != nil {
		logger.Error(context.Background(), "数据库初始化失败，主节点无法启动", slog.String("error", err.Error()))
		os.Exit(1)
	}
	walTruncateThreshold, err := config.ParseBytes("database.wal_truncate_threshold", cfg.Database.WALTruncateThreshold, true)
	if err != nil {
		logger.Error(context.Background(), "数据库 WAL 配置无效", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer closeDatabaseWithCheckpoint(cfg, database, walTruncateThreshold, logger)
	logger.Info(context.Background(), "主节点数据库迁移已完成")
	startDatabaseMaintenance(cfg, database, walTruncateThreshold, logger)
	archive := newAccountingArchive(cfg, logger)

	runtime := mastercontrol.NewRuntimeStore()
	tokenSigner, err := downloadtoken.NewSignerFromPrivateFile(cfg.DownloadToken.SigningPrivateKeyFile)
	if err != nil {
		logger.Error(context.Background(), "下载令牌签发私钥加载失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	repo := mastercontrol.Repository{
		DB: database, Logger: logger, Runtime: runtime,
		Archive:                    archive,
		ReplicationSigner:          tokenSigner,
		PublicProbeNetworkFailures: cfg.Node.PublicProbeNetworkFailures,
	}
	projectLoader := mirrorsync.NewProjectLoader(*projectsPath, projects)
	syncService := startMirrorSync(cfg, projectLoader, database, runtime, logger)
	publicServer, err := newPublicServer(cfg, quota, notices, projects, *projectsPath,
		*noticesPath, location, database, runtime, logger, tokenSigner, archive)
	if err != nil {
		logger.Error(context.Background(), "公共下载链路初始化失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	startControlServices(cfg, repo, logger)
	startAdminService(cfg, repo, syncService, projectLoader, &publicServer, logger)

	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Ready: func() bool { return true }, Version: version,
	}, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	mux.Handle("/", requestid.Middleware(publicServer.Handler(), cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
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

func startMirrorSync(cfg config.Master, projects *mirrorsync.ProjectLoader, db *sql.DB, runtime *mastercontrol.RuntimeStore, logger *logging.Logger) mirrorsync.Service {
	interval, _ := time.ParseDuration(cfg.Scan.Interval)
	token := ""
	if cfg.Scan.GitHubTokenEnv != "" {
		token = os.Getenv(cfg.Scan.GitHubTokenEnv)
	}
	store := mirrorsync.Store{DB: db, Runtime: runtime}
	service := mirrorsync.Service{
		Scanner: mirrorsync.Scanner{
			Store: store, GitHub: mirrorsync.HTTPGitHubClient{
				Client: &http.Client{Timeout: mirrorsync.DefaultGitHubClientTimeout},
				Token:  token,
			}, Logger: logger,
		},
		Projects: projects, Interval: interval, Logger: logger,
	}
	ctx := context.Background()
	go service.Run(ctx)
	logger.Info(ctx, "Release 扫描调度已启动", slog.String("interval", cfg.Scan.Interval))
	return service
}

func newPublicServer(cfg config.Master, quota config.Quota, notices config.Notices,
	projects config.Projects, projectsPath, noticesPath string, loc *time.Location,
	db *sql.DB, runtime *mastercontrol.RuntimeStore, logger *logging.Logger,
	signer downloadtoken.Signer, archive *accountingarchive.Writer) (public.Server, error) {
	altchaTTL, _ := time.ParseDuration(cfg.ALTCHA.ChallengeTTL)
	apiTTL, _ := time.ParseDuration(cfg.APIPoW.ChallengeTTL)
	firstConnectionTimeout, _ := time.ParseDuration(cfg.DownloadToken.FirstConnectionTimeout)
	idleTimeout, _ := time.ParseDuration(cfg.DownloadToken.IdleTimeout)
	maxDuration, _ := time.ParseDuration(cfg.DownloadToken.MaxDuration)
	tokenLifetime := public.TokenLifetime{
		FirstConnectionTimeout: firstConnectionTimeout,
		IdleTimeout:            idleTimeout,
		MaxDuration:            maxDuration,
	}
	logger.Info(context.Background(), "公共下载链路已启用")
	server, err := public.New(db, signer, altchaTTL, apiTTL, tokenLifetime,
		cfg.ALTCHA.Difficulty, cfg.APIPoW.LeadingZeroBits, quota, loc,
		cfg.Proxy.TrustedCIDRs, projects, projectsPath, noticesPath, notices.Notices, runtime, logger,
		cfg.Node.PublicProbeNetworkFailures, archive)
	if err != nil {
		return public.Server{}, err
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_ = server.Store.SampleNodeAvailability(context.Background())
		}
	}()
	return server, nil
}

func startControlServices(cfg config.Master, repo mastercontrol.Repository, logger *logging.Logger) {
	timeout, _ := time.ParseDuration(cfg.Node.HeartbeatTimeout)
	grace, _ := time.ParseDuration(cfg.Node.HeartbeatOfflineGrace)
	interval, _ := time.ParseDuration(cfg.Node.HeartbeatInterval)
	probes := publicProbeService(cfg, repo, logger)
	if cfg.Server.ControlListen != "" && cfg.Node.TLS.CertFile != "" && cfg.Node.TLS.KeyFile != "" {
		tlsCfg, err := controltls.ControlServer(cfg.Node.TLS.CertFile, cfg.Node.TLS.KeyFile, cfg.Node.TLS.ClientCAFile)
		startTLSListener(cfg.Server.ControlListen, tlsCfg, err, logger, mastercontrol.ControlServer{
			Repo: repo, HeartbeatInterval: interval, HeartbeatTimeout: timeout, Logger: logger,
			PublicProbes: probes,
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
	startHeartbeatSweep(repo, timeout, grace, logger)
}

func publicProbeService(cfg config.Master, repo mastercontrol.Repository,
	logger *logging.Logger) *mastercontrol.PublicProbeService {
	if cfg.Node.PublicProbeEnabled == nil || !*cfg.Node.PublicProbeEnabled {
		return nil
	}
	interval, _ := time.ParseDuration(cfg.Node.PublicProbeInterval)
	timeout, _ := time.ParseDuration(cfg.Node.PublicProbeTimeout)
	ttl, _ := time.ParseDuration(cfg.Node.PublicProbeTTL)
	return &mastercontrol.PublicProbeService{Repo: repo, Logger: logger,
		Config: mastercontrol.PublicProbeConfig{Enabled: true,
			Interval: interval, Timeout: timeout, TTL: ttl,
			NetworkFailures: cfg.Node.PublicProbeNetworkFailures}}
}
