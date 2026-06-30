package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
)

const (
	defaultWALAutocheckpointPages = 1000
	defaultWALSizeLimitBytes      = 256 * 1024 * 1024
)

type walSettings struct {
	AutocheckpointPages    int
	JournalSizeLimitBytes  int64
	TruncateThresholdBytes int64
}

type WALCheckpointResult struct {
	Mode          string
	Busy          int
	LogFrames     int
	CheckedFrames int
	WALSizeBytes  int64
}

func defaultWALSettings() walSettings {
	return walSettings{
		AutocheckpointPages:    defaultWALAutocheckpointPages,
		JournalSizeLimitBytes:  defaultWALSizeLimitBytes,
		TruncateThresholdBytes: defaultWALSizeLimitBytes,
	}
}

func WALPath(dbPath string) string {
	return dbPath + "-wal"
}

func WALSize(dbPath string) (int64, bool, error) {
	info, err := os.Stat(WALPath(dbPath))
	if err == nil {
		return info.Size(), true, nil
	}
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	return 0, false, err
}

func logWALFile(logger versionLogFunc, dbPath string, threshold int64) {
	if logger == nil {
		return
	}
	size, exists, err := WALSize(dbPath)
	if err != nil {
		logger(ctx(), "SQLite WAL 状态读取失败",
			slog.String("wal_path", WALPath(dbPath)),
			slog.String("error", err.Error()))
		return
	}
	if !exists || size == 0 {
		return
	}
	attrs := []slog.Attr{
		slog.String("wal_path", WALPath(dbPath)),
		slog.Int64("wal_size_bytes", size),
	}
	if threshold > 0 {
		attrs = append(attrs, slog.Int64("truncate_threshold_bytes", threshold))
	}
	logger(ctx(), "SQLite WAL 文件已存在，启动前将尝试检查点收敛", attrs...)
}

func CheckpointWAL(db *sql.DB, dbPath string, truncateThreshold int64, logger versionLogFunc) error {
	passive, err := runWALCheckpoint(db, dbPath, "PASSIVE")
	if err != nil {
		return err
	}
	if shouldLogWALCheckpoint(passive, truncateThreshold) {
		message := "SQLite WAL checkpoint 完成"
		if incompleteWALCheckpoint(passive) {
			message = "SQLite WAL checkpoint 未完全收敛"
		}
		logWALCheckpoint(logger, message, passive, truncateThreshold)
	}
	if passive.Busy != 0 || incompleteWALCheckpoint(passive) ||
		truncateThreshold <= 0 || passive.WALSizeBytes < truncateThreshold {
		return nil
	}
	truncated, err := runWALCheckpoint(db, dbPath, "TRUNCATE")
	if err != nil {
		return err
	}
	logWALCheckpoint(logger, "SQLite WAL truncate checkpoint 完成", truncated, truncateThreshold)
	return nil
}

func runWALCheckpoint(db *sql.DB, dbPath, mode string) (WALCheckpointResult, error) {
	var result WALCheckpointResult
	result.Mode = mode
	err := db.QueryRow(fmt.Sprintf("PRAGMA wal_checkpoint(%s)", mode)).
		Scan(&result.Busy, &result.LogFrames, &result.CheckedFrames)
	if err != nil {
		return result, fmt.Errorf("执行 SQLite WAL checkpoint 失败：%w", err)
	}
	size, _, statErr := WALSize(dbPath)
	if statErr != nil {
		return result, fmt.Errorf("读取 SQLite WAL 文件大小失败：%w", statErr)
	}
	result.WALSizeBytes = size
	return result, nil
}

func shouldLogWALCheckpoint(result WALCheckpointResult, threshold int64) bool {
	return result.Busy != 0 || result.LogFrames > 0 ||
		(threshold > 0 && result.WALSizeBytes >= threshold)
}

func incompleteWALCheckpoint(result WALCheckpointResult) bool {
	return result.LogFrames > 0 && result.CheckedFrames < result.LogFrames
}

func logWALCheckpoint(logger versionLogFunc, message string, result WALCheckpointResult, threshold int64) {
	if logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("checkpoint_mode", result.Mode),
		slog.Int("busy", result.Busy),
		slog.Int("log_frames", result.LogFrames),
		slog.Int("checked_frames", result.CheckedFrames),
		slog.Int64("wal_size_bytes", result.WALSizeBytes),
	}
	if threshold > 0 {
		attrs = append(attrs, slog.Int64("truncate_threshold_bytes", threshold))
	}
	logger(context.Background(), message, attrs...)
}
