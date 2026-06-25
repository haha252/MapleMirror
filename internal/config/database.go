package config

import "errors"

type Database struct {
	Path                   string `yaml:"path"`
	BusyTimeout            string `yaml:"busy_timeout"`
	WAL                    *bool  `yaml:"wal"`
	WALAutocheckpointPages int    `yaml:"wal_autocheckpoint_pages"`
	WALJournalSizeLimit    string `yaml:"wal_journal_size_limit"`
	WALTruncateThreshold   string `yaml:"wal_truncate_threshold"`
	WALCheckpointInterval  string `yaml:"wal_checkpoint_interval"`
}

func applyDatabaseDefaults(c *Master, warn WarnFunc) {
	setString(&c.Database.Path, "data/master.db", "database.path", warn)
	setString(&c.Database.BusyTimeout, "5s", "database.busy_timeout", warn)
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
