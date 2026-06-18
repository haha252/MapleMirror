package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	nodeupgrades "mirror-server/internal/storage/upgrades/node"
)

//go:embed migrations/master/*.sql migrations/node/*.sql
var schemaFiles embed.FS

func applyDatabaseVersion(db *sql.DB, kind string) error {
	plan, err := versionPlan(kind)
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
			if err := applyV1Schema(ctx(), tx, plan.SchemaPattern); err != nil {
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
	if plan.Kind == databaseKindNode && version == 1 {
		if err := nodeupgrades.EnsureV1IdentityMaterials(ctx(), tx); err != nil {
			return err
		}
	}
	if _, err := applyVersionPlan(ctx(), tx, plan, version); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交数据库版本事务失败：%w", err)
	}
	return nil
}
