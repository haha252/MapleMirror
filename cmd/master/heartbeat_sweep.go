package main

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
)

func startHeartbeatSweep(repo mastercontrol.Repository, timeout time.Duration, logger *logging.Logger) {
	if timeout <= 0 {
		return
	}
	interval := timeout / 2
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			result, err := repo.SweepOffline(context.Background(), timeout)
			if err != nil {
				if logger != nil {
					logger.Warn(context.Background(), "节点离线巡检失败",
						slog.String("heartbeat_timeout", timeout.String()),
						slog.String("error", err.Error()))
				}
				continue
			}
			if logger == nil {
				continue
			}
			if result.OfflineNodes == 0 {
				logger.Debug(context.Background(), "节点离线巡检完成",
					slog.String("heartbeat_timeout", timeout.String()),
					slog.Int64("offline_nodes", 0),
					slog.Int64("active_delayed_nodes", result.ActiveDelayedNodes))
				continue
			}
			logger.Warn(context.Background(), "节点心跳超时，已标记离线",
				slog.String("heartbeat_timeout", timeout.String()),
				slog.Int64("offline_nodes", result.OfflineNodes),
				slog.Int64("active_delayed_nodes", result.ActiveDelayedNodes))
		}
	}()
}
