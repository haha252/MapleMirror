package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"
)

//go:embed migrations/master/*.sql migrations/node/*.sql
var schemaFiles embed.FS

const (
	databaseKindMaster = "master"
	databaseKindNode   = "node"
	masterDBVersion    = 1
	nodeDBVersion      = 1
)

type versionUpgrade struct {
	From  int
	To    int
	Apply func(context.Context, *sql.Tx) error
}

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

func versionPlan(kind string) (int, string, []versionUpgrade, error) {
	switch kind {
	case databaseKindMaster:
		return masterDBVersion, "migrations/master/*.sql", nil, nil
	case databaseKindNode:
		return nodeDBVersion, "migrations/node/*.sql", nil, nil
	default:
		return 0, "", nil, fmt.Errorf("未知数据库类型 %s", kind)
	}
}

func currentVersion(tx *sql.Tx, kind string) (int, bool, error) {
	var version int
	err := tx.QueryRow(`SELECT version FROM database_version WHERE kind = ?`, kind).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("读取数据库版本失败：%w", err)
	}
	return version, true, nil
}

func setVersion(tx *sql.Tx, kind string, version int) error {
	_, err := tx.Exec(`INSERT INTO database_version(kind, version, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET version = excluded.version,
		updated_at = excluded.updated_at`,
		kind, version, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("记录数据库版本失败：%w", err)
	}
	return nil
}

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

func ensureNodeV1IdentityMaterials(ctx context.Context, tx *sql.Tx) error {
	columns := []struct {
		name string
		def  string
	}{
		{"certificate_pem", "TEXT"},
		{"ca_pem", "TEXT"},
		{"private_key_pem", "TEXT"},
		{"download_token_public_key_pem", "TEXT"},
	}
	for _, column := range columns {
		ok, err := hasColumn(ctx, tx, "control_identity", column.name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`ALTER TABLE control_identity ADD COLUMN `+column.name+` `+column.def); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS node_enrollment_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enrollment_id TEXT,
		pairing_code TEXT,
		private_key_pem TEXT,
		ca_pem TEXT,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	ok, err := hasColumn(ctx, tx, "inventory_report_cursor", "force_report_requested_at")
	if err != nil || ok {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`ALTER TABLE inventory_report_cursor ADD COLUMN force_report_requested_at TEXT`)
	return err
}

func hasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func nextUpgrade(upgrades []versionUpgrade, from int) *versionUpgrade {
	for i := range upgrades {
		if upgrades[i].From == from && upgrades[i].To == from+1 {
			return &upgrades[i]
		}
	}
	return nil
}

func ctx() context.Context { return context.Background() }
