package main

import (
	"context"
	"database/sql"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/public"
	"mirror-server/internal/master/statbuffer"
)

func newPublicServer(cfg config.Master, quota config.Quota, notices config.Notices,
	projects config.Projects, filters config.Filters,
	projectsPath, filtersPath, noticesPath, changelogPath string, loc *time.Location,
	db *sql.DB, runtime *mastercontrol.RuntimeStore, logger *logging.Logger,
	signer downloadtoken.Signer, archive *accountingarchive.Writer,
	statsBuffer *statbuffer.Buffer) (public.Server, error) {
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
		cfg.Proxy.TrustedCIDRs, projects, filters, projectsPath, filtersPath,
		noticesPath, changelogPath, notices.Notices, runtime, logger,
		cfg.Node.PublicProbeNetworkFailures, *cfg.Server.CatalogBatchRows,
		*cfg.Server.CatalogPrefetchRemainingRows, archive, statsBuffer)
	if err != nil {
		return public.Server{}, err
	}
	go sampleNodeAvailability(server)
	return server, nil
}

func sampleNodeAvailability(server public.Server) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		_ = server.Store.SampleNodeAvailability(context.Background())
	}
}
