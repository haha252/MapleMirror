package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

func applicationSchemaEmpty(tx *sql.Tx) (bool, error) {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type IN ('table', 'index', 'trigger', 'view')
		AND name NOT LIKE 'sqlite_%'
		AND name NOT IN ('database_version', 'schema_migrations')`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查数据库结构失败：%w", err)
	}
	return count == 0, nil
}

func applyV1Schema(ctx context.Context, tx *sql.Tx, pattern string) error {
	names, err := fs.Glob(schemaFiles, pattern)
	if err != nil {
		return fmt.Errorf("读取 v1 数据库结构清单失败：%w", err)
	}
	sort.Strings(names)
	for _, name := range names {
		statement, err := schemaFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("读取 v1 数据库结构文件失败：%w", err)
		}
		if _, err := tx.ExecContext(ctx, string(statement)); err != nil {
			return fmt.Errorf("执行 v1 数据库结构 %s 失败：%w", filepath.Base(name), err)
		}
	}
	return nil
}

func adoptLegacyV1(ctx context.Context, tx *sql.Tx, kind string) error {
	var err error
	switch kind {
	case databaseKindMaster:
		err = nil
	case databaseKindNode:
		err = ensureNodeV1IdentityMaterials(ctx, tx)
	default:
		return fmt.Errorf("未知数据库类型 %s", kind)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS schema_migrations`)
	return err
}
