package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func applyVersionPlan(ctx context.Context, tx *sql.Tx, plan databaseVersionPlan, version int) (int, error) {
	for version < plan.CurrentVersion {
		upgrade := nextUpgrade(plan.Upgrades, version)
		if upgrade == nil {
			return version, fmt.Errorf("%s 数据库缺少 v%d -> v%d 升级器", plan.Kind, version, version+1)
		}
		if err := upgrade.Apply(ctx, tx); err != nil {
			return version, fmt.Errorf("执行 %s 数据库 v%d -> v%d 升级失败：%w",
				plan.Kind, upgrade.From, upgrade.To, err)
		}
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
