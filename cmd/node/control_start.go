package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/node/activity"
	"mirror-server/internal/node/capacity"
	nodecontrol "mirror-server/internal/node/control"
	"mirror-server/internal/node/eventwake"
	nodeprobe "mirror-server/internal/node/probe"
	"mirror-server/internal/node/swarmstate"
	"mirror-server/internal/node/syncer"
)

func startControlClient(cfg config.Node, db *sql.DB, logger *logging.Logger,
	probes *nodeprobe.Store, eventWake *eventwake.Notifier, swarmRegistry *swarmstate.Registry, activityCounters *activity.Counters) {
	if err := nodecontrol.RecoverInterruptedLocalTasks(db); err != nil {
		logger.Error(context.Background(), "恢复进程重启前的同步任务失败", slog.String("error", err.Error()))
		return
	}
	addressHost := ""
	if cfg.Master.ControlAddress != "" {
		address, err := url.Parse(cfg.Master.ControlAddress)
		if err != nil {
			logger.Error(context.Background(), "主节点控制地址无效", slog.String("error", err.Error()))
			return
		}
		addressHost = address.Host
	}
	capacityManager := capacity.NewManager(cfg.Storage.Directory, cfg.Storage.TempDirectory)
	syncLimiter := syncer.NewBandwidthLimiter(cfg.Sync.BandwidthLimitBPS)
	baseHTTPClient := &http.Client{Timeout: syncer.DefaultHTTPClientTimeout}
	sourceHTTPClient := syncer.NewSourceHTTPClient(baseHTTPClient, false, 10*time.Second)
	supervisor := controlSupervisor{
		cfg: cfg, db: db, logger: logger, address: addressHost, wsURL: cfg.Master.ControlWSAddress, version: version,
		executor: syncer.Executor{DB: db, Storage: cfg.Storage.Directory,
			TempDir: cfg.Storage.TempDirectory, Logger: logger,
			Client:                    baseHTTPClient,
			SourceClient:              sourceHTTPClient,
			Probe:                     syncer.NewSourceProbe(nil),
			BandwidthLimitBPS:         cfg.Sync.BandwidthLimitBPS,
			SyncLimiter:               syncLimiter,
			ForcePeerDownload:         cfg.Sync.ForcePeerDownload,
			PeerFallbackMaxConcurrent: cfg.Sync.PeerFallbackMaxConcurrent,
			PeerFallbackWorkers:       cfg.Sync.PeerFallbackWorkers,
			PeerFallbackMinSize:       cfg.Sync.PeerFallbackMinSizeBytes,
			Capacity:                  capacityManager,
			Swarm:                     swarmRegistry},
		limiter:   nodecontrol.NewTaskLimiter(cfg.Sync.MaxWorkers),
		bandwidth: nodecontrol.NewNetworkBandwidthSampler(),
		capacity:  capacityManager,
		activity:  activityCounters,
		eventWake: eventWake,
		swarm:     swarmRegistry,
		probes:    probes,
		v2Runtime: nodecontrol.NewV2Runtime(),
	}
	go supervisor.run()
	target := addressHost
	if supervisor.wsURL != "" {
		target = supervisor.wsURL
	}
	logger.Info(context.Background(), "节点主动控制连接已启动", slog.String("master", target))
}
