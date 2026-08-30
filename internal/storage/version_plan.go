package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	masterupgrades "mirror-server/internal/storage/upgrades/master"
	nodeupgrades "mirror-server/internal/storage/upgrades/node"
)

const (
	databaseKindMaster = "master"
	databaseKindNode   = "node"
	masterDBVersion    = 19
	nodeDBVersion      = 5
)

type databaseVersionPlan struct {
	Kind           string
	CurrentVersion int
	SchemaPattern  string
	Upgrades       []versionUpgrade
}

type versionUpgrade struct {
	From  int
	To    int
	Apply func(context.Context, *sql.Tx) error
}

func versionPlan(kind string) (databaseVersionPlan, error) {
	switch kind {
	case databaseKindMaster:
		return databaseVersionPlan{
			Kind:           databaseKindMaster,
			CurrentVersion: masterDBVersion,
			SchemaPattern:  "migrations/master/*.sql",
			Upgrades: []versionUpgrade{
				{From: 1, To: 2, Apply: masterupgrades.V1ToV2},
				{From: 2, To: 3, Apply: masterupgrades.V2ToV3},
				{From: 3, To: 4, Apply: masterupgrades.V3ToV4},
				{From: 4, To: 5, Apply: masterupgrades.V4ToV5},
				{From: 5, To: 6, Apply: masterupgrades.V5ToV6},
				{From: 6, To: 7, Apply: masterupgrades.V6ToV7},
				{From: 7, To: 8, Apply: masterupgrades.V7ToV8},
				{From: 8, To: 9, Apply: masterupgrades.V8ToV9},
				{From: 9, To: 10, Apply: masterupgrades.V9ToV10},
				{From: 10, To: 11, Apply: masterupgrades.V10ToV11},
				{From: 11, To: 12, Apply: masterupgrades.V11ToV12},
				{From: 12, To: 13, Apply: masterupgrades.V12ToV13},
				{From: 13, To: 14, Apply: masterupgrades.V13ToV14},
				{From: 14, To: 15, Apply: masterupgrades.V14ToV15},
				{From: 15, To: 16, Apply: masterupgrades.V15ToV16},
				{From: 16, To: 17, Apply: masterupgrades.V16ToV17},
				{From: 17, To: 18, Apply: masterupgrades.V17ToV18},
				{From: 18, To: 19, Apply: masterupgrades.V18ToV19},
			},
		}, nil
	case databaseKindNode:
		return databaseVersionPlan{
			Kind:           databaseKindNode,
			CurrentVersion: nodeDBVersion,
			SchemaPattern:  "migrations/node/*.sql",
			Upgrades: []versionUpgrade{
				{From: 1, To: 2, Apply: nodeupgrades.V1ToV2},
				{From: 2, To: 3, Apply: nodeupgrades.V2ToV3},
				{From: 3, To: 4, Apply: nodeupgrades.V3ToV4},
				{From: 4, To: 5, Apply: nodeupgrades.V4ToV5},
			},
		}, nil
	default:
		return databaseVersionPlan{}, fmt.Errorf("未知数据库类型 %s", kind)
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
