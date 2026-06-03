package config

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Projects struct {
	Projects []Project `yaml:"projects"`
}

type Project struct {
	ID                         string     `yaml:"id"`
	Name                       string     `yaml:"name"`
	Repository                 string     `yaml:"repository"`
	IconPath                   string     `yaml:"icon_path"`
	Enabled                    bool       `yaml:"enabled"`
	RetainVersions             int        `yaml:"retain_versions"`
	IncludePrerelease          bool       `yaml:"include_prerelease"`
	DownloadMultiplier         int        `yaml:"download_multiplier"`
	AssetInclude               AssetRules `yaml:"asset_include"`
	AssetExclude               AssetRules `yaml:"asset_exclude"`
	ArchitectureMatchEnabled   bool       `yaml:"architecture_match_enabled"`
	ArchitectureRegex          string     `yaml:"architecture_regex"`
	ArchitectureDefaultEnabled bool       `yaml:"architecture_default_enabled"`
	SystemMatchEnabled         bool       `yaml:"system_match_enabled"`
	SystemRegex                string     `yaml:"system_regex"`
	ResolvedIconPath           string     `yaml:"-"`
}

type AssetRule struct {
	Pattern  string `yaml:"pattern"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
}

type AssetRules []AssetRule

func (r *AssetRules) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode {
		rules := make([]AssetRule, 0, len(value.Content))
		for _, item := range value.Content {
			rule, err := decodeAssetRule(item)
			if err != nil {
				return err
			}
			rules = append(rules, rule)
		}
		*r = rules
		return nil
	}
	if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
		*r = nil
		return nil
	}
	return errors.New("资产匹配规则必须是列表")
}

func decodeAssetRule(node *yaml.Node) (AssetRule, error) {
	if node.Kind == yaml.ScalarNode {
		return AssetRule{Pattern: node.Value, Type: "glob"}, nil
	}
	var rule AssetRule
	if node.Kind != yaml.MappingNode {
		return rule, errors.New("资产匹配规则必须是字符串或对象")
	}
	if err := node.Decode(&rule); err != nil {
		return rule, err
	}
	if rule.Type == "" {
		rule.Type = "glob"
	}
	return rule, nil
}

func LoadProjects(path string, warn WarnFunc) (Projects, error) {
	var c Projects
	if err := readYAML(path, &c, ProjectsExample); err != nil {
		return c, err
	}
	baseDir := filepath.Dir(path)
	known := map[string]bool{}
	for i := range c.Projects {
		p := &c.Projects[i]
		if p.RetainVersions == 0 {
			p.RetainVersions = 3
			warnDefault(warn, "projects[].retain_versions", "3")
		}
		if p.DownloadMultiplier == 0 {
			p.DownloadMultiplier = 1
			warnDefault(warn, "projects[].download_multiplier", "1")
		}
		resolved, err := resolveProjectIconPath(baseDir, p.IconPath)
		if err != nil {
			return c, err
		}
		p.ResolvedIconPath = resolved
		if err := validateProject(*p, known); err != nil {
			return c, err
		}
		known[p.ID] = true
	}
	return c, nil
}

func validateProject(p Project, known map[string]bool) error {
	if p.ID == "" || p.Name == "" || known[p.ID] {
		return errors.New("项目 id 和中文名称必须存在，且 id 不得重复")
	}
	parts := strings.Split(p.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(p.Repository, "://") {
		return errors.New("项目 repository 必须使用公开 GitHub 仓库的 owner/repo 格式")
	}
	if p.RetainVersions <= 0 || p.DownloadMultiplier <= 0 {
		return errors.New("项目保留版本数和下载倍率必须大于零")
	}
	if p.ArchitectureMatchEnabled {
		if strings.TrimSpace(p.ArchitectureRegex) == "" {
			return fmt.Errorf("项目 %s 启用架构匹配时必须配置 architecture_regex", p.ID)
		}
		if _, err := regexp.Compile(p.ArchitectureRegex); err != nil {
			return fmt.Errorf("项目 %s 的架构提取正则无效：%w", p.ID, err)
		}
	}
	if err := validateAssetRules(p.ID, "asset_include", p.AssetInclude); err != nil {
		return err
	}
	if err := validateAssetRules(p.ID, "asset_exclude", p.AssetExclude); err != nil {
		return err
	}
	if p.SystemMatchEnabled {
		if strings.TrimSpace(p.SystemRegex) == "" {
			return fmt.Errorf("项目 %s 启用系统匹配时必须配置 system_regex", p.ID)
		}
		if _, err := regexp.Compile(p.SystemRegex); err != nil {
			return fmt.Errorf("项目 %s 的系统提取正则无效：%w", p.ID, err)
		}
	}
	return nil
}

func validateAssetRules(projectID, field string, rules AssetRules) error {
	for _, rule := range rules {
		if strings.TrimSpace(rule.Pattern) == "" {
			return fmt.Errorf("项目 %s 的 %s 规则 pattern 不能为空", projectID, field)
		}
		switch rule.Type {
		case "", "glob":
			if _, err := path.Match(rule.Pattern, ""); err != nil {
				return fmt.Errorf("项目 %s 的 %s glob 规则无效：%w", projectID, field, err)
			}
		case "regex":
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				return fmt.Errorf("项目 %s 的 %s 正则规则无效：%w", projectID, field, err)
			}
		default:
			return fmt.Errorf("项目 %s 的 %s 规则 type 必须为 glob 或 regex", projectID, field)
		}
	}
	return nil
}

func resolveProjectIconPath(baseDir, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	clean := filepath.Clean(strings.TrimSpace(value))
	if filepath.IsAbs(clean) {
		return "", errors.New("项目 icon_path 必须使用相对路径")
	}
	if clean == "." || clean == "" {
		return "", errors.New("项目 icon_path 不能为空路径")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("项目 icon_path 不得越级访问配置目录")
	}
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
	default:
		return "", errors.New("项目 icon_path 只允许 png、jpg、jpeg、gif、webp、svg 图片")
	}
	return filepath.Join(baseDir, clean), nil
}
