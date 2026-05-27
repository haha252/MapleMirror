package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"
)

//go:embed migrations/master/*.sql migrations/node/*.sql
var migrationFiles embed.FS

func applyMigrations(db *sql.DB, pattern string) error {
	names, err := fs.Glob(migrationFiles, pattern)
	if err != nil {
		return fmt.Errorf("读取数据库迁移清单失败：%w", err)
	}
	sort.Strings(names)
	transaction, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始数据库迁移事务失败：%w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("创建迁移记录表失败：%w", err)
	}
	for _, name := range names {
		applied, err := alreadyApplied(transaction, filepath.Base(name))
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		statement, err := migrationFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("读取数据库迁移文件失败：%w", err)
		}
		if _, err := transaction.Exec(string(statement)); err != nil {
			return fmt.Errorf("执行数据库迁移 %s 失败：%w", filepath.Base(name), err)
		}
		if _, err := transaction.Exec(
			"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
			filepath.Base(name), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("记录数据库迁移失败：%w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交数据库迁移失败：%w", err)
	}
	return nil
}

func alreadyApplied(transaction *sql.Tx, version string) (bool, error) {
	var count int
	err := transaction.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查数据库迁移版本失败：%w", err)
	}
	return count > 0, nil
}
