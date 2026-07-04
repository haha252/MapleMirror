package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

type databaseWatchdog struct {
	ready atomic.Bool
	fatal chan error
}

func startDatabaseWatchdog(cfg config.Database, db *sql.DB, logger *logging.Logger) *databaseWatchdog {
	interval, _ := time.ParseDuration(cfg.HealthCheckInterval)
	timeout, _ := time.ParseDuration(cfg.HealthCheckTimeout)
	w := &databaseWatchdog{fatal: make(chan error, 1)}
	w.ready.Store(true)
	go w.run(context.Background(), interval, timeout, cfg.HealthFailureThreshold, func(ctx context.Context) error {
		var schemaVersion int
		return db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schemaVersion)
	}, logger)
	logger.Info(context.Background(), "数据库健康看门狗已启动",
		slog.Duration("interval", interval),
		slog.Duration("timeout", timeout),
		slog.Int("failure_threshold", cfg.HealthFailureThreshold))
	return w
}

func (w *databaseWatchdog) Ready() bool {
	return w != nil && w.ready.Load()
}

func (w *databaseWatchdog) Fatal() <-chan error {
	if w == nil {
		return nil
	}
	return w.fatal
}

type watchdogLogger interface {
	Warn(context.Context, string, ...slog.Attr)
}

func (w *databaseWatchdog) run(ctx context.Context, interval, timeout time.Duration, threshold int,
	check func(context.Context) error, logger watchdogLogger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			err := check(checkCtx)
			cancel()
			if err == nil {
				consecutiveFailures = 0
				continue
			}
			consecutiveFailures++
			if logger != nil {
				logger.Warn(context.Background(), "数据库健康检查失败",
					slog.Int("consecutive_failures", consecutiveFailures),
					slog.Int("failure_threshold", threshold),
					slog.String("error", err.Error()))
			}
			if consecutiveFailures < threshold {
				continue
			}
			w.ready.Store(false)
			w.fatal <- fmt.Errorf("数据库连续 %d 次健康检查失败：%w", consecutiveFailures, err)
			return
		}
	}
}
