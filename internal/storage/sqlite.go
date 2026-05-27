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

func OpenMaster(cfg config.Database) (*sql.DB, error) {
	timeout, err := time.ParseDuration(cfg.BusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("数据库等待时间无效：%w", err)
	}
	wal := cfg.WAL != nil && *cfg.WAL
	return open(cfg.Path, timeout, wal, "migrations/master/*.sql")
}

func OpenNode(path string) (*sql.DB, error) {
	return open(path, 5*time.Second, true, "migrations/node/*.sql")
}

func open(path string, timeout time.Duration, wal bool, pattern string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败：%w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败：%w", err)
	}
	db.SetMaxOpenConns(1)
	if err := configure(db, timeout, wal); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applyMigrations(db, pattern); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func configure(db *sql.DB, timeout time.Duration, wal bool) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		fmt.Sprintf("PRAGMA busy_timeout = %d", timeout.Milliseconds()),
	}
	if wal {
		statements = append(statements, "PRAGMA journal_mode = WAL")
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("设置 SQLite 运行参数失败：%w", err)
		}
	}
	return nil
}
