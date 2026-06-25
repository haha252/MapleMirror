package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/config"

	_ "modernc.org/sqlite"
)

func OpenMaster(cfg config.Database, options ...OpenOption) (*sql.DB, error) {
	timeout, err := time.ParseDuration(cfg.BusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("数据库等待时间无效：%w", err)
	}
	wal := cfg.WAL != nil && *cfg.WAL
	walSettings, err := masterWALSettings(cfg)
	if err != nil {
		return nil, err
	}
	return open(cfg.Path, timeout, wal, walSettings, databaseKindMaster, collectOpenOptions(options))
}

func OpenNode(path string, options ...OpenOption) (*sql.DB, error) {
	return open(path, 5*time.Second, true, defaultWALSettings(), databaseKindNode, collectOpenOptions(options))
}

func open(path string, timeout time.Duration, wal bool, walSettings walSettings, kind string, opts openOptions) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败：%w", err)
	}
	if wal {
		logWALFile(opts.versionLogger, path, walSettings.TruncateThresholdBytes)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败：%w", err)
	}
	db.SetMaxOpenConns(1)
	if err := configure(db, timeout, wal, walSettings); err != nil {
		_ = db.Close()
		return nil, err
	}
	if wal {
		if err := CheckpointWAL(db, path, walSettings.TruncateThresholdBytes, opts.versionLogger); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := applyDatabaseVersion(db, kind, opts); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func configure(db *sql.DB, timeout time.Duration, wal bool, walSettings walSettings) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		fmt.Sprintf("PRAGMA busy_timeout = %d", timeout.Milliseconds()),
	}
	if wal {
		statements = append(statements, "PRAGMA journal_mode = WAL")
		statements = append(statements,
			fmt.Sprintf("PRAGMA wal_autocheckpoint = %d", walSettings.AutocheckpointPages),
			fmt.Sprintf("PRAGMA journal_size_limit = %d", walSettings.JournalSizeLimitBytes))
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("设置 SQLite 运行参数失败：%w", err)
		}
	}
	return nil
}

func masterWALSettings(cfg config.Database) (walSettings, error) {
	settings := defaultWALSettings()
	if cfg.WALAutocheckpointPages < 0 {
		return walSettings{}, fmt.Errorf("配置字段 database.wal_autocheckpoint_pages 不得为负数")
	}
	if cfg.WALAutocheckpointPages != 0 {
		settings.AutocheckpointPages = cfg.WALAutocheckpointPages
	}
	if cfg.WALJournalSizeLimit != "" {
		size, err := config.ParseBytes("database.wal_journal_size_limit", cfg.WALJournalSizeLimit, true)
		if err != nil {
			return walSettings{}, err
		}
		settings.JournalSizeLimitBytes = size
	}
	if cfg.WALTruncateThreshold != "" {
		size, err := config.ParseBytes("database.wal_truncate_threshold", cfg.WALTruncateThreshold, true)
		if err != nil {
			return walSettings{}, err
		}
		settings.TruncateThresholdBytes = size
	}
	return settings, nil
}
