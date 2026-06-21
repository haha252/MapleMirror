package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

func applyVersionPlan(ctx context.Context, tx *sql.Tx, plan databaseVersionPlan, version int,
	logger versionLogFunc) (int, error) {
	for version < plan.CurrentVersion {
		upgrade := nextUpgrade(plan.Upgrades, version)
		if upgrade == nil {
			return version, fmt.Errorf("%s 数据库缺少 v%d -> v%d 升级器", plan.Kind, version, version+1)
		}
		logVersionUpgrade(logger, ctx, "数据库升级器开始执行", plan.Kind, upgrade)
		if err := upgrade.Apply(ctx, tx); err != nil {
			return version, fmt.Errorf("执行 %s 数据库 v%d -> v%d 升级失败：%w",
				plan.Kind, upgrade.From, upgrade.To, err)
		}
		logVersionUpgrade(logger, ctx, "数据库升级器执行完成", plan.Kind, upgrade)
		version = upgrade.To
		if err := setVersion(tx, plan.Kind, version); err != nil {
			return version, err
		}
	}
	if version > plan.CurrentVersion {
		return version, fmt.Errorf("%s 数据库版本 %d 高于当前程序支持的版本 %d",
			plan.Kind, version, plan.CurrentVersion)
	}
	return version, nil
}

func logVersionUpgrade(logger versionLogFunc, ctx context.Context, message, kind string, upgrade *versionUpgrade) {
	if logger == nil {
		return
	}
	logger(ctx, message,
		slog.String("database_kind", kind),
		slog.Int("from_version", upgrade.From),
		slog.Int("to_version", upgrade.To))
}
