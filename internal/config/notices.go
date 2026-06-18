package config

import (
	"fmt"
	"strings"
)

type PublicNotice struct {
	Level   string `yaml:"level"`
	Message string `yaml:"message"`
}

type Notices struct {
	Notices []PublicNotice `yaml:"notices"`
}

func LoadNotices(path string, warn WarnFunc) (Notices, error) {
	var c Notices
	data, repaired, err := readYAMLWithRepair(path, &c, NoticesExample, NoticesExample)
	if err != nil {
		return c, err
	}
	if err := validatePublicNotices(c.Notices); err != nil {
		return c, err
	}
	return c, writeRepairedYAML(path, data, repaired)
}

func validatePublicNotices(notices []PublicNotice) error {
	for i, notice := range notices {
		level := strings.TrimSpace(notice.Level)
		switch level {
		case "info", "notice", "warn", "critical":
		default:
			return fmt.Errorf("配置字段 notices[%d].level 必须为 info、notice、warn 或 critical", i)
		}
		if strings.TrimSpace(notice.Message) == "" {
			return fmt.Errorf("配置字段 notices[%d].message 不能为空", i)
		}
	}
	return nil
}

func LoadNoticesOnly(path string) ([]PublicNotice, error) {
	c, err := LoadNotices(path, nil)
	if err != nil {
		return nil, err
	}
	return c.Notices, nil
}
