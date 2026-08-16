package main

import (
	"context"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/indexnow"
	"mirror-server/internal/logging"
)

const publicSEORevision = "seo-2026-08-16-v1"

func startIndexNow(cfg config.Master, version string, logger *logging.Logger) *indexnow.Manager {
	if cfg.IndexNow.Enabled == nil || !*cfg.IndexNow.Enabled {
		return nil
	}
	timeout, err := time.ParseDuration(cfg.IndexNow.Timeout)
	if err != nil {
		logger.Error(context.Background(), "IndexNow 请求超时配置无效", slog.String("error", err.Error()))
		return nil
	}
	manager, err := indexnow.New(indexnow.Options{
		BaseURL: cfg.Server.PublicBaseURL, Endpoint: cfg.IndexNow.Endpoint,
		KeyFile: cfg.IndexNow.KeyFile, StateFile: cfg.IndexNow.StateFile,
		Timeout: timeout, Logger: logger,
	})
	if err != nil {
		logger.Error(context.Background(), "IndexNow 初始化失败，通知功能已停用", slog.String("error", err.Error()))
		return nil
	}
	logger.Info(context.Background(), "IndexNow 通知已启用",
		slog.String("host", "fyhub.cn"), slog.String("key_path", manager.KeyPath()))
	return manager
}

func bootstrapIndexNow(manager *indexnow.Manager, projects config.Projects, version string) {
	if manager == nil {
		return
	}
	projectIDs := make([]string, 0, len(projects.Projects))
	for _, project := range projects.Projects {
		if project.Enabled {
			projectIDs = append(projectIDs, project.ID)
		}
	}
	manager.Bootstrap(projectIDs, publicSEORevision+"|"+version)
}
