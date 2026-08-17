package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/indexnow"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/mirrorsync"
)

const publicSEORevision = "seo-2026-08-16-v1"

func startIndexNow(cfg config.Master, location *time.Location, logger *logging.Logger) (*indexnow.Manager, *logging.Logger) {
	if location == nil {
		location = time.Local
	}
	indexLogger, err := newIndexNowLogger(cfg.Logging, location)
	if err != nil {
		if logger != nil {
			logger.Error(context.Background(), "IndexNow 独立日志初始化失败，已回退主节点日志",
				slog.String("error", err.Error()))
		}
		return nil, nil
	}
	indexLogger.Info(context.Background(), "IndexNow 独立日志初始化成功",
		slog.String("directory", indexNowLogDirectory(cfg.Logging.Directory)))
	if cfg.IndexNow.Enabled == nil || !*cfg.IndexNow.Enabled {
		indexLogger.Info(context.Background(), "IndexNow 通知已禁用")
		return nil, indexLogger
	}
	timeout, err := time.ParseDuration(cfg.IndexNow.Timeout)
	if err != nil {
		indexLogger.Error(context.Background(), "IndexNow 请求超时配置无效", slog.String("error", err.Error()))
		return nil, indexLogger
	}
	manager, err := indexnow.New(indexnow.Options{
		BaseURL: cfg.Server.PublicBaseURL, Endpoint: cfg.IndexNow.Endpoint,
		KeyFile: cfg.IndexNow.KeyFile, StateFile: cfg.IndexNow.StateFile,
		Timeout: timeout, Location: location, Logger: indexLogger,
	})
	if err != nil {
		indexLogger.Error(context.Background(), "IndexNow 初始化失败，通知功能已停用", slog.String("error", err.Error()))
		return nil, indexLogger
	}
	indexLogger.Info(context.Background(), "IndexNow 通知已启用",
		slog.String("host", manager.Host()), slog.String("endpoint", cfg.IndexNow.Endpoint),
		slog.Duration("timeout", timeout), slog.String("schedule", "debounce_5m"),
		slog.String("timezone", location.String()))
	return manager, indexLogger
}

func newIndexNowLogger(cfg config.Logging, location *time.Location) (*logging.Logger, error) {
	cfg.Directory = indexNowLogDirectory(cfg.Directory)
	return logging.New("indexnow", cfg, location, io.Discard)
}

func indexNowLogDirectory(masterDirectory string) string {
	clean := filepath.Clean(masterDirectory)
	return filepath.Join(filepath.Dir(clean), "indexnow")
}

func bootstrapIndexNow(manager *indexnow.Manager, scanner mirrorsync.Scanner, projects config.Projects, logger *logging.Logger) {
	if manager == nil {
		return
	}
	snapshot, err := scanner.PublicSnapshot(context.Background(), projects)
	if err != nil {
		if logger != nil {
			logger.Warn(context.Background(), "IndexNow 公开页面快照初始化失败，稍后由 Release 扫描重试",
				slog.String("error", err.Error()))
		}
		return
	}
	manager.ReconcileSnapshot(context.Background(), snapshot)
}
