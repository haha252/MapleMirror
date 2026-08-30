package config

import "errors"

type Archive struct {
	Enabled *bool  `yaml:"enabled"`
	Root    string `yaml:"root"`
}

func applyArchiveDefaults(c *Master, warn WarnFunc) {
	if c.Archive.Enabled == nil {
		value := true
		c.Archive.Enabled = &value
		warnDefault(warn, "archive.enabled", "true")
	}
	setString(&c.Archive.Root, "logs", "archive.root", warn)
}

func validateArchive(c Archive) error {
	if c.Enabled != nil && *c.Enabled && c.Root == "" {
		return errors.New("启用归档时 archive.root 不得为空")
	}
	return nil
}
