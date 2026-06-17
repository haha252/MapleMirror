package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	databaseKindMaster = "master"
	databaseKindNode   = "node"
	masterDBVersion    = 1
	nodeDBVersion      = 3
)

type versionUpgrade struct {
	From  int
	To    int
	Apply func(context.Context, *sql.Tx) error
}

func versionPlan(kind string) (int, string, []versionUpgrade, error) {
	switch kind {
	case databaseKindMaster:
		return masterDBVersion, "migrations/master/*.sql", nil, nil
	case databaseKindNode:
		return nodeDBVersion, "migrations/node/*.sql", []versionUpgrade{
			{From: 1, To: 2, Apply: upgradeNode1To2},
			{From: 2, To: 3, Apply: upgradeNode2To3},
		}, nil
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

func nextUpgrade(upgrades []versionUpgrade, from int) *versionUpgrade {
	for i := range upgrades {
		if upgrades[i].From == from && upgrades[i].To == from+1 {
			return &upgrades[i]
		}
	}
	return nil
}

func ctx() context.Context { return context.Background() }
