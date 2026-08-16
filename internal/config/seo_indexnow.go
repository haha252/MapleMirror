package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type IndexNow struct {
	Enabled   *bool  `yaml:"enabled"`
	Endpoint  string `yaml:"endpoint"`
	KeyFile   string `yaml:"key_file"`
	StateFile string `yaml:"state_file"`
	Timeout   string `yaml:"timeout"`
}

func applySEOIndexNowDefaults(c *Master, warn WarnFunc) {
	setString(&c.Server.PublicBaseURL, "https://fyhub.cn", "server.public_base_url", warn)
	if c.IndexNow.Enabled == nil {
		enabled := true
		c.IndexNow.Enabled = &enabled
		warnDefault(warn, "indexnow.enabled", "true")
	}
	setString(&c.IndexNow.Endpoint, "https://api.indexnow.org/indexnow", "indexnow.endpoint", warn)
	setString(&c.IndexNow.KeyFile, "secrets/indexnow.key", "indexnow.key_file", warn)
	setString(&c.IndexNow.StateFile, "secrets/indexnow.state", "indexnow.state_file", warn)
	setString(&c.IndexNow.Timeout, "10s", "indexnow.timeout", warn)
}

func validateSEOIndexNow(c Master) error {
	base, err := url.Parse(strings.TrimSpace(c.Server.PublicBaseURL))
	if err != nil || base.Scheme == "" || base.Hostname() == "" ||
		(base.Scheme != "http" && base.Scheme != "https") || base.User != nil ||
		base.Path != "" && base.Path != "/" || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("配置字段 server.public_base_url 必须是没有路径、查询参数和片段的 http/https URL")
	}
	if !strings.EqualFold(base.Hostname(), "fyhub.cn") {
		return errors.New("配置字段 server.public_base_url 必须使用 fyhub.cn，不支持其他主机名或 www.fyhub.cn")
	}
	if c.IndexNow.Enabled == nil || !*c.IndexNow.Enabled {
		return nil
	}
	if base.Scheme != "https" {
		return errors.New("启用 IndexNow 时 server.public_base_url 必须使用 HTTPS")
	}
	endpoint, err := url.Parse(strings.TrimSpace(c.IndexNow.Endpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil {
		return errors.New("配置字段 indexnow.endpoint 必须是 HTTPS URL")
	}
	if strings.TrimSpace(c.IndexNow.KeyFile) == "" || strings.TrimSpace(c.IndexNow.StateFile) == "" {
		return errors.New("启用 IndexNow 时 key_file 和 state_file 不得为空")
	}
	if err := validDuration("indexnow.timeout", c.IndexNow.Timeout); err != nil {
		return fmt.Errorf("IndexNow 请求超时配置无效：%w", err)
	}
	return nil
}
