package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
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
