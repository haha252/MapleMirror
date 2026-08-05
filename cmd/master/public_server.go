package main

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/geoip"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/powtelemetry"
	"mirror-server/internal/master/public"
	"mirror-server/internal/master/statbuffer"
)

func newPublicServer(cfg config.Master, configPath string, quota config.Quota, notices config.Notices,
	projects config.Projects, filters config.Filters,
	projectsPath, filtersPath, noticesPath, changelogPath string, loc *time.Location,
	db *sql.DB, runtime *mastercontrol.RuntimeStore, logger *logging.Logger,
	regionClassifier geoip.Classifier, signer downloadtoken.Signer, archive *accountingarchive.Writer,
	statsBuffer *statbuffer.Buffer) (public.Server, error) {
	vdfTTL, _ := time.ParseDuration(cfg.VDF.ChallengeTTL)
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
	server, err := public.New(db, signer, vdfTTL, apiTTL, tokenLifetime,
		cfg.PoWSizeTiers, cfg.VDFSizeTiers, cfg.VDF,
		cfg.APIPoW.V1Enabled != nil && *cfg.APIPoW.V1Enabled, quota, loc,
		cfg.Proxy.TrustedCIDRs, projects, filters, projectsPath, filtersPath,
		noticesPath, changelogPath, notices.Notices, configPath, runtime, regionClassifier, logger,
		cfg.Node.PublicProbeNetworkFailures, *cfg.Server.CatalogBatchRows,
		*cfg.Server.CatalogPrefetchRemainingRows, archive, statsBuffer)
	if err != nil {
		return public.Server{}, err
	}
	telemetry, telemetryErr := powtelemetry.New(powTelemetryDirectory(cfg.Logging.Directory),
		cfg.Logging.RetentionDays, loc)
	if telemetryErr != nil {
		logger.Warn(context.Background(), "PoW 遥测日志初始化失败，不影响下载授权",
			slog.String("error", telemetryErr.Error()))
	} else {
		server.SetPoWTelemetry(telemetry)
	}
	go sampleNodeAvailability(server)
	return server, nil
}

func startCountryIPManager(cfg config.Master, logger *logging.Logger) *geoip.Manager {
	client, _, err := newGitHubHTTPClient(cfg.Scan)
	if err != nil {
		logger.Warn(context.Background(), "国家 IP 数据库客户端配置无效，地区加权已停用",
			slog.String("error", err.Error()))
		return nil
	}
	cachePath := filepath.Join(filepath.Dir(filepath.Clean(cfg.Database.Path)), "country-ip-blocks.json")
	manager, err := geoip.New(geoip.Options{
		Client: client, CachePath: cachePath, Logger: logger,
	})
	if err != nil {
		logger.Warn(context.Background(), "国家 IP 数据库初始化失败，地区加权已停用",
			slog.String("error", err.Error()))
		return nil
	}
	manager.Start(context.Background())
	logger.Info(context.Background(), "国家 IP 数据库更新调度已启动",
		slog.String("cache_path", cachePath), slog.String("interval", "24h"))
	return manager
}

func powTelemetryDirectory(masterLogDirectory string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(masterLogDirectory)), "pow")
}

func sampleNodeAvailability(server public.Server) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		_ = server.Store.SampleNodeAvailability(context.Background())
	}
}
