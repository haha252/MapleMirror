package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	"mirror-server/internal/storage"
)

func runAccountingArchiveMigration(cfg config.Master, db *sql.DB, walTruncateThreshold int64,
	logger *logging.Logger) {
	ctx := context.Background()
	result, err := accountingarchive.MigrateTrafficEvents(ctx, db,
		accountingarchive.TrafficMigrationOptions{Root: cfg.Archive.Root})
	if err != nil {
		logger.Error(ctx, "旧流量明细归档迁移失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := accountingarchive.WriteTrafficManifest(cfg.Archive.Root, result); err != nil {
		logger.Error(ctx, "旧流量明细归档摘要写入失败", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := storage.CheckpointWAL(db, cfg.Database.Path, walTruncateThreshold, logger.Info); err != nil {
		logger.Warn(ctx, "归档迁移后 WAL checkpoint 失败", slog.String("error", err.Error()))
	}
	logger.Info(ctx, "旧流量明细归档迁移完成", slog.Int64("archived_rows", result.ArchivedRows))
}
