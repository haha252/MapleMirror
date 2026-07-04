package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrExampleCreated = errors.New("已生成示例配置，请确认安全字段后重新启动")

type WarnFunc func(field, value string)

type Logging struct {
	ConsoleLevel  string `yaml:"console_level"`
	FileLevel     string `yaml:"file_level"`
	Directory     string `yaml:"directory"`
	RetentionDays int    `yaml:"retention_days"`
	MaxFileSizeMB int64  `yaml:"max_file_size_mb"`
}

const DefaultLogMaxFileSizeMB int64 = 1024

const deprecatedWarningPrefix = "deprecated:"

func readYAML(path string, target any, example []byte) ([]byte, error) {
	data, _, err := readYAMLWithRepair(path, target, example, example)
	return data, err
}

func readYAMLWithRepair(path string, target any, example, repairExample []byte,
	migrations ...yamlMigration) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, false, fmt.Errorf("创建配置目录失败：%w", err)
		}
		if err := os.WriteFile(path, example, 0o600); err != nil {
			return nil, false, fmt.Errorf("生成示例配置失败：%w", err)
		}
		return nil, false, ErrExampleCreated
	}
	if err != nil {
		return nil, false, fmt.Errorf("读取配置失败：%w", err)
	}
	data, repaired, err := repairYAML(data, repairExample, migrations...)
	if err != nil {
		return nil, false, err
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		return nil, false, fmt.Errorf("解析 YAML 配置失败：%w", err)
	}
	return data, repaired, nil
}

func updateYAMLScalars(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置失败：%w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("解析 YAML 配置失败：%w", err)
	}
	for dotted, value := range values {
		setYAMLScalar(&doc, strings.Split(dotted, "."), value)
	}
	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("编码 YAML 配置失败：%w", err)
	}
	return replaceConfigFile(path, encoded)
}

func setYAMLScalar(node *yaml.Node, path []string, value string) {
	if len(path) == 0 {
		node.Kind = yaml.ScalarNode
		node.Tag = "!!str"
		node.Value = value
		return
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		}
		setYAMLScalar(node.Content[0], path, value)
		return
	}
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		node.Content = nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == path[0] {
			setYAMLScalar(node.Content[i+1], path[1:], value)
			return
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}
	child := &yaml.Node{}
	node.Content = append(node.Content, key, child)
	setYAMLScalar(child, path[1:], value)
}

func writeExample(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建示例配置目录失败：%w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func warnDefault(warn WarnFunc, field, value string) {
	if warn != nil {
		warn(field, value)
	}
}

func warnDeprecated(warn WarnFunc, field, replacement string) {
	if warn != nil {
		warn(field, deprecatedWarningPrefix+replacement)
	}
}

func DeprecatedWarningMessage(value string) (string, bool) {
	if strings.HasPrefix(value, deprecatedWarningPrefix) {
		return strings.TrimPrefix(value, deprecatedWarningPrefix), true
	}
	return "", false
}

func applyLoggingDefaults(c *Logging, directory string, warn WarnFunc) {
	if c.ConsoleLevel == "" {
		c.ConsoleLevel = "info"
		warnDefault(warn, "logging.console_level", "info")
	}
	if c.FileLevel == "" {
		c.FileLevel = "info"
		warnDefault(warn, "logging.file_level", "info")
	}
	if c.Directory == "" {
		c.Directory = directory
		warnDefault(warn, "logging.directory", directory)
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = 30
		warnDefault(warn, "logging.retention_days", "30")
	}
	if c.MaxFileSizeMB == 0 {
		c.MaxFileSizeMB = DefaultLogMaxFileSizeMB
		warnDefault(warn, "logging.max_file_size_mb", strconv.FormatInt(DefaultLogMaxFileSizeMB, 10))
	}
}

func validateLogging(c Logging) error {
	for field, value := range map[string]string{"console_level": c.ConsoleLevel, "file_level": c.FileLevel} {
		switch value {
		case "debug", "info", "warn", "error":
		default:
			return fmt.Errorf("日志字段 logging.%s 必须为 debug、info、warn 或 error", field)
		}
	}
	if c.RetentionDays <= 0 {
		return errors.New("日志字段 logging.retention_days 必须大于零")
	}
	if c.MaxFileSizeMB <= 0 {
		return errors.New("日志字段 logging.max_file_size_mb 必须大于零")
	}
	if c.MaxFileSizeMB > int64(^uint64(0)>>1)/(1024*1024) {
		return errors.New("日志字段 logging.max_file_size_mb 过大")
	}
	return nil
}

func validListen(value string) bool {
	_, _, err := net.SplitHostPort(value)
	return err == nil
}

func loopbackListen(value string) bool {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func validDuration(field, value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fmt.Errorf("配置字段 %s 必须为正持续时间", field)
	}
	return nil
}

func parseGiB(field, value string) (int64, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || parts[1] != "GiB" {
		return 0, fmt.Errorf("配置字段 %s 必须使用 GiB 格式", field)
	}
	count, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("配置字段 %s 必须为正容量", field)
	}
	return count * 1024 * 1024 * 1024, nil
}

func ParseBytes(field, value string, allowZero bool) (int64, error) {
	value = strings.TrimSpace(value)
	if allowZero && value == "0" {
		return 0, nil
	}
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return 0, fmt.Errorf("配置字段 %s 必须使用 B、KiB、MiB 或 GiB 格式", field)
	}
	count, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("配置字段 %s 必须为正容量", field)
	}
	factor, ok := byteUnitFactor(parts[1])
	if !ok || count > (1<<63-1)/factor {
		return 0, fmt.Errorf("配置字段 %s 必须使用 B、KiB、MiB 或 GiB 格式", field)
	}
	return count * factor, nil
}

func byteUnitFactor(unit string) (int64, bool) {
	switch unit {
	case "B":
		return 1, true
	case "KiB":
		return 1024, true
	case "MiB":
		return 1024 * 1024, true
	case "GiB":
		return 1024 * 1024 * 1024, true
	default:
		return 0, false
	}
}
