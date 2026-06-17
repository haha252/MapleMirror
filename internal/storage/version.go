package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
)

//go:embed migrations/master/*.sql migrations/node/*.sql
var schemaFiles embed.FS

func applyDatabaseVersion(db *sql.DB, kind string) error {
	target, pattern, upgrades, err := versionPlan(kind)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("开始数据库版本事务失败：%w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS database_version (
		kind TEXT PRIMARY KEY,
		version INTEGER NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("创建数据库版本表失败：%w", err)
	}
	version, ok, err := currentVersion(tx, kind)
	if err != nil {
		return err
	}
	if !ok {
		empty, err := applicationSchemaEmpty(tx)
		if err != nil {
			return err
		}
		if empty {
			if err := applyV1Schema(ctx(), tx, pattern); err != nil {
				return err
			}
		} else if err := adoptLegacyV1(ctx(), tx, kind); err != nil {
			return err
		}
		version = 1
		if err := setVersion(tx, kind, version); err != nil {
			return err
		}
	}
	if kind == databaseKindNode && version == 1 {
		if err := ensureNodeV1IdentityMaterials(ctx(), tx); err != nil {
			return err
		}
	}
	for version < target {
		upgrade := nextUpgrade(upgrades, version)
		if upgrade == nil {
			return fmt.Errorf("%s 数据库缺少 %d 到 %d 的升级器", kind, version, version+1)
		}
		if err := upgrade.Apply(ctx(), tx); err != nil {
			return fmt.Errorf("执行 %s 数据库 %d 到 %d 升级失败：%w",
				kind, upgrade.From, upgrade.To, err)
		}
		version = upgrade.To
		if err := setVersion(tx, kind, version); err != nil {
			return err
		}
	}
	if version > target {
		return fmt.Errorf("%s 数据库版本 %d 高于当前程序支持的版本 %d", kind, version, target)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交数据库版本事务失败：%w", err)
	}
	return nil
}
