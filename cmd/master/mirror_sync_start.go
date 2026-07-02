package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
)

func startMirrorSync(cfg config.Master, projects *mirrorsync.ProjectLoader,
	db *sql.DB, runtime *mastercontrol.RuntimeStore, logger *logging.Logger) mirrorsync.Service {
	interval, _ := time.ParseDuration(cfg.Scan.Interval)
	token := ""
	if cfg.Scan.GitHubTokenEnv != "" {
		token = os.Getenv(cfg.Scan.GitHubTokenEnv)
	}
	githubClient, githubTimeout, err := newGitHubHTTPClient(cfg.Scan)
	if err != nil {
		logger.Error(context.Background(), "GitHub Release HTTP 客户端配置无效", slog.String("error", err.Error()))
		githubTimeout = mirrorsync.DefaultGitHubClientTimeout
	}
	store := mirrorsync.Store{DB: db, Runtime: runtime}
	service := mirrorsync.Service{
		Scanner: mirrorsync.Scanner{
			Store: store, GitHub: mirrorsync.HTTPGitHubClient{
				Client:  githubClient,
				Token:   token,
				Timeout: githubTimeout,
			}, Logger: logger,
		},
		Projects: projects, Interval: interval, Logger: logger,
	}
	ctx := context.Background()
	go service.Run(ctx)
	logger.Info(ctx, "Release 扫描调度已启动", slog.String("interval", cfg.Scan.Interval))
	return service
}
