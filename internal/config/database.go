package config

import (
	"errors"
	"fmt"
	"time"
)

type Database struct {
	Path                   string `yaml:"path"`
	BusyTimeout            string `yaml:"busy_timeout"`
	HealthCheckInterval    string `yaml:"health_check_interval"`
	HealthCheckTimeout     string `yaml:"health_check_timeout"`
	HealthFailureThreshold int    `yaml:"health_failure_threshold"`
	WAL                    *bool  `yaml:"wal"`
	WALAutocheckpointPages int    `yaml:"wal_autocheckpoint_pages"`
	WALJournalSizeLimit    string `yaml:"wal_journal_size_limit"`
	WALTruncateThreshold   string `yaml:"wal_truncate_threshold"`
	WALCheckpointInterval  string `yaml:"wal_checkpoint_interval"`
}

func applyDatabaseDefaults(c *Master, warn WarnFunc) {
	setString(&c.Database.Path, "data/master.db", "database.path", warn)
	setString(&c.Database.BusyTimeout, "5s", "database.busy_timeout", warn)
	setString(&c.Database.HealthCheckInterval, "10s", "database.health_check_interval", warn)
	setString(&c.Database.HealthCheckTimeout, "3s", "database.health_check_timeout", warn)
	if c.Database.HealthFailureThreshold == 0 {
		c.Database.HealthFailureThreshold = 3
		warnDefault(warn, "database.health_failure_threshold", "3")
	}
	if c.Database.WAL == nil {
		value := true
		c.Database.WAL = &value
		warnDefault(warn, "database.wal", "true")
	}
	if c.Database.WALAutocheckpointPages == 0 {
		c.Database.WALAutocheckpointPages = 1000
		warnDefault(warn, "database.wal_autocheckpoint_pages", "1000")
	}
	setString(&c.Database.WALJournalSizeLimit, "256 MiB", "database.wal_journal_size_limit", warn)
	setString(&c.Database.WALTruncateThreshold, "256 MiB", "database.wal_truncate_threshold", warn)
	setString(&c.Database.WALCheckpointInterval, "5m", "database.wal_checkpoint_interval", warn)
}

func validateDatabase(c Database) error {
	if c.WALAutocheckpointPages < 0 {
		return errors.New("配置字段 database.wal_autocheckpoint_pages 不得为负数")
	}
	if c.HealthFailureThreshold < 1 {
		return errors.New("配置字段 database.health_failure_threshold 必须大于零")
	}
	for field, value := range map[string]string{
		"database.health_check_interval": c.HealthCheckInterval,
		"database.health_check_timeout":  c.HealthCheckTimeout,
	} {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("配置字段 %s 时长无效：%w", field, err)
		}
		if duration <= 0 {
			return fmt.Errorf("配置字段 %s 必须大于零", field)
		}
	}
	for field, value := range map[string]string{
		"database.wal_journal_size_limit": c.WALJournalSizeLimit,
		"database.wal_truncate_threshold": c.WALTruncateThreshold,
	} {
		if _, err := ParseBytes(field, value, true); err != nil {
			return err
		}
	}
	return nil
}
