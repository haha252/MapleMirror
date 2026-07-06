package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	"mirror-server/internal/storage"
)

func startDatabaseMaintenance(cfg config.Master, db *sql.DB, walTruncateThreshold int64, logger *logging.Logger) {
	go runDailyDataMaintenance(cfg, db, logger)
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

func runDailyDataMaintenance(cfg config.Master, db *sql.DB, logger *logging.Logger) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 4, 0, 0, 0, now.Location())
		time.Sleep(time.Until(next))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		err := maintainOnlineData(ctx, db)
		cancel()
		if err != nil {
			logger.Warn(context.Background(), "每日数据库维护失败", slog.String("error", err.Error()))
		} else {
			logger.Info(context.Background(), "每日数据库维护完成")
		}
		if err := accountingarchive.Maintain(cfg.Archive.Root, cfg.Logging.RetentionDays, time.Now()); err != nil {
			logger.Warn(context.Background(), "归档日志维护失败", slog.String("error", err.Error()))
		}
	}
}

func maintainOnlineData(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`WITH ranked AS (SELECT id, ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY connected_at DESC, id DESC) rn FROM node_control_sessions) DELETE FROM node_control_sessions WHERE id IN (SELECT id FROM ranked WHERE rn > 1)`,
		`WITH ranked AS (SELECT id, ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY reported_at DESC, revision DESC, id DESC) rn FROM node_inventory_reports) DELETE FROM node_inventory_reports WHERE id IN (SELECT id FROM ranked WHERE rn > 1)`,
		`DELETE FROM node_availability_samples`,
		`DELETE FROM node_availability_rollups WHERE bucket_start < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-35 days')`,
		`WITH ranked AS (SELECT id, ROW_NUMBER() OVER (PARTITION BY COALESCE(project_id, '') ORDER BY started_at DESC, id DESC) rn FROM sync_scans WHERE completed_at IS NOT NULL) DELETE FROM sync_scans WHERE id IN (SELECT id FROM ranked WHERE rn > 100)`,
		`DELETE FROM traffic_event_dedupe WHERE EXISTS (SELECT 1 FROM node_traffic_cursors c WHERE c.node_id = traffic_event_dedupe.node_id AND traffic_event_dedupe.event_sequence < c.last_event_sequence - 10000)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `PRAGMA optimize`)
	return err
}

func closeDatabaseWithCheckpoint(cfg config.Master, db *sql.DB, walTruncateThreshold int64, logger *logging.Logger) {
	if cfg.Database.WAL != nil && *cfg.Database.WAL {
		if err := storage.CheckpointWAL(db, cfg.Database.Path, walTruncateThreshold, logger.Info); err != nil {
			logger.Warn(context.Background(), "主节点退出前 WAL checkpoint 失败", slog.String("error", err.Error()))
		}
	}
	_ = db.Close()
}
