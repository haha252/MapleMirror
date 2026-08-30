package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	"mirror-server/internal/master/accountingarchive"
	"mirror-server/internal/storage"
)

const walStallObservationsBeforeFatal = 2

type walCheckpointMonitor struct {
	checkedFrames           int
	consecutiveObservations int
}

func startDatabaseMaintenance(cfg config.Master, db *sql.DB, walTruncateThreshold int64,
	watchdog *databaseWatchdog, logger *logging.Logger) {
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
		var monitor walCheckpointMonitor
		for range ticker.C {
			result, err := storage.CheckpointWALResult(
				db, cfg.Database.Path, walTruncateThreshold, logger.Info)
			if err != nil {
				logger.Warn(context.Background(), "数据库 WAL checkpoint 失败", slog.String("error", err.Error()))
				continue
			}
			if err := monitor.Observe(result, walTruncateThreshold); err != nil {
				logger.Error(context.Background(), "数据库 WAL 长时间无法收敛，即将退出以释放陈旧读快照",
					slog.String("error", err.Error()),
					slog.Int("log_frames", result.LogFrames),
					slog.Int("checked_frames", result.CheckedFrames),
					slog.Int64("wal_size_bytes", result.WALSizeBytes))
				watchdog.Fail(err)
				return
			}
		}
	}()
	logger.Info(context.Background(), "数据库 WAL checkpoint 后台维护已启动",
		slog.String("interval", cfg.Database.WALCheckpointInterval))
}

func (m *walCheckpointMonitor) Observe(result storage.WALCheckpointResult, truncateThreshold int64) error {
	incomplete := result.LogFrames > 0 && result.CheckedFrames < result.LogFrames
	if !incomplete {
		m.checkedFrames = 0
		m.consecutiveObservations = 0
		return nil
	}
	if result.CheckedFrames != m.checkedFrames {
		m.checkedFrames = result.CheckedFrames
		m.consecutiveObservations = 1
		return nil
	}
	m.consecutiveObservations++
	if truncateThreshold <= 0 || result.WALSizeBytes < truncateThreshold ||
		m.consecutiveObservations < walStallObservationsBeforeFatal {
		return nil
	}
	return fmt.Errorf("SQLite WAL 连续 %d 次停留在第 %d 帧且已增长至 %d 字节",
		m.consecutiveObservations, result.CheckedFrames, result.WALSizeBytes)
}

func runDailyDataMaintenance(cfg config.Master, db *sql.DB, logger *logging.Logger) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 4, 0, 0, 0, now.Location())
		time.Sleep(time.Until(next))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		err := maintainOnlineData(ctx, db, cfg.History.DownloadRetentionDays)
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

func maintainOnlineData(ctx context.Context, db *sql.DB, downloadHistoryRetentionDays int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if downloadHistoryRetentionDays <= 0 {
		downloadHistoryRetentionDays = 7
	}
	historyCutoff := time.Now().UTC().AddDate(0, 0, -downloadHistoryRetentionDays).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `DELETE FROM download_history WHERE issued_at < ?`, historyCutoff); err != nil {
		return err
	}
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
