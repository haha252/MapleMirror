package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/storage"
)

func startDatabaseMaintenance(cfg config.Master, db *sql.DB, walTruncateThreshold int64, logger *logging.Logger) {
	if cfg.Database.WAL == nil || !*cfg.Database.WAL {
		return
	}
	interval, err := time.ParseDuration(cfg.Database.WALCheckpointInterval)
	if err != nil || interval <= 0 {
		logger.Warn(context.Background(), "数据库 WAL checkpoint 间隔无效，后台维护未启动",
			slog.String("value", cfg.Database.WALCheckpointInterval))
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := storage.CheckpointWAL(db, cfg.Database.Path, walTruncateThreshold, logger.Info); err != nil {
				logger.Warn(context.Background(), "数据库 WAL checkpoint 失败", slog.String("error", err.Error()))
			}
		}
	}()
	logger.Info(context.Background(), "数据库 WAL checkpoint 后台维护已启动",
		slog.String("interval", cfg.Database.WALCheckpointInterval))
}

func closeDatabaseWithCheckpoint(cfg config.Master, db *sql.DB, walTruncateThreshold int64, logger *logging.Logger) {
	if cfg.Database.WAL != nil && *cfg.Database.WAL {
		if err := storage.CheckpointWAL(db, cfg.Database.Path, walTruncateThreshold, logger.Info); err != nil {
			logger.Warn(context.Background(), "主节点退出前 WAL checkpoint 失败", slog.String("error", err.Error()))
		}
	}
	_ = db.Close()
}
