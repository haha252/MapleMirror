package config

import "errors"

type History struct {
	DownloadRetentionDays int `yaml:"download_retention_days"`
}

func applyHistoryDefaults(c *Master, warn WarnFunc) {
	if c.History.DownloadRetentionDays == 0 {
		c.History.DownloadRetentionDays = 7
		warnDefault(warn, "history.download_retention_days", "7")
	}
}

func validateHistory(history History) error {
	if history.DownloadRetentionDays <= 0 {
		return errors.New("history.download_retention_days 必须大于零")
	}
	return nil
}
