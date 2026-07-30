package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"

	"mirror-server/internal/bootstrap"
	"mirror-server/internal/buildinfo"
	"mirror-server/internal/config"
	"mirror-server/internal/controltls"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/health"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/master/statbuffer"
	"mirror-server/internal/requestid"
	"mirror-server/internal/storage"
)

var version = "寮€鍙戠増"

func main() {
	path := flag.String("config", "config.yaml", "主节点配置文件路径")
	projectsPath := flag.String("projects", "projects.yaml", "项目清单配置文件路径")
	quotaPath := flag.String("quota", "quota.yaml", "额度配置文件路径")
	noticesPath := flag.String("notices", "notices.yaml", "公告配置文件路径")
	filtersPath := flag.String("filters", "filters.yaml", "首页筛选配置文件路径")
	changelogPath := flag.String("changelog", "changelog", "更新日志目录路径")
	archiveAccounting := flag.Bool("archive-accounting", false, "归档旧数据库明细并收缩在线状态")
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
	filters, err := config.LoadFilters(*filtersPath, warn)
	if handleLoad(err, "首页筛选配置", &created) {
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
		fmt.Fprintln(os.Stderr, "已生成主节点所需示例配置和安全材料，请确认项目、额度、公告和筛选配置后重新启动。")
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
	exitCode := 0
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()
	defer logger.Close()
	logger.Info(context.Background(), "主节点程序启动", buildinfo.Attributes(version)...)
	for _, item := range warnings {
		logger.ConfigWarning(item[0], item[1])
	}

	database, err := storage.OpenMaster(cfg.Database,
		storage.WithVersionLogger(logger.Info),
		storage.WithSQLDebugLogger(logger.Debug))
	if err != nil {
		logger.Error(context.Background(), "数据库初始化失败，主节点无法启动", slog.String("error", err.Error()))
		os.Exit(1)
	}
	walTruncateThreshold, err := config.ParseBytes("database.wal_truncate_threshold", cfg.Database.WALTruncateThreshold, true)
	if err != nil {
		logger.Error(context.Background(), "数据库 WAL 配置无效", slog.String("error", err.Error()))
		os.Exit(1)
	}
	databaseFailed := false
	defer func() {
		if databaseFailed {
			return
		}
		closeDatabaseWithCheckpoint(cfg, database, walTruncateThreshold, logger)
	}()
	logger.Info(context.Background(), "主节点数据库迁移已完成")
	if *archiveAccounting {
		runAccountingArchiveMigration(cfg, database, walTruncateThreshold, logger)
		return
	}
	warning, err := accountingarchive.TrafficArchiveWarning(context.Background(), database)
	if err != nil {
		logger.Error(context.Background(), "旧流量明细归档状态检查失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if warning != "" {
		logger.Warn(context.Background(), "旧流量明细归档未完成", slog.String("detail", warning))
	}
	databaseWatchdog := startDatabaseWatchdog(cfg.Database, database, logger)
	startDatabaseMaintenance(cfg, database, walTruncateThreshold, databaseWatchdog, logger)
	archive := newAccountingArchive(cfg, logger)
	statsBuffer := statbuffer.New(database, 2*time.Second)
	statsBuffer.Start(context.Background())
	defer statsBuffer.Stop()

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
		StatsBuffer:                statsBuffer,
	}
	projectLoader := mirrorsync.NewProjectLoader(*projectsPath, projects)
	syncService := startMirrorSync(cfg, projectLoader, database, runtime, logger)
	publicServer, err := newPublicServer(cfg, quota, notices, projects, filters,
		*projectsPath, *filtersPath, *noticesPath, *changelogPath, location, database, runtime,
		logger, tokenSigner, archive, statsBuffer)
	if err != nil {
		logger.Error(context.Background(), "公共下载链路初始化失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer publicServer.Close()
	startControlServices(cfg, repo, logger)
	startAdminService(cfg, repo, syncService, projectLoader, &publicServer, logger)

	mux := http.NewServeMux()
	mux.Handle("/healthz", requestid.Middleware(health.Handler{
		Logger: logger, Ready: databaseWatchdog.Ready, Version: version,
	}, cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	mux.Handle("/", requestid.Middleware(publicServer.Handler(), cfg.RequestID.ResponseHeader, cfg.RequestID.ParentHeader))
	if err := runServer(cfg.Server.PublicListen, mux, databaseWatchdog.Fatal(), logger); err != nil {
		logger.Error(context.Background(), "主节点运行时致命错误，即将退出", slog.String("error", err.Error()))
		databaseFailed = !databaseWatchdog.Ready()
		exitCode = 1
	}
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
