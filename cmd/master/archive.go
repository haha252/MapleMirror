package main

import (
	"context"
	"log/slog"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
)

func newAccountingArchive(cfg config.Master, logger *logging.Logger) *accountingarchive.Writer {
	if cfg.Archive.Enabled != nil && !*cfg.Archive.Enabled {
		logger.Info(context.Background(), "明细事件归档已禁用")
		return nil
	}
	writer := accountingarchive.New(cfg.Archive.Root)
	if writer != nil {
		logger.Info(context.Background(), "明细事件归档已启用", slog.String("root", cfg.Archive.Root))
	}
	return writer
}
