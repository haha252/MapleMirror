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
	ID                         string        `yaml:"id"`
	Name                       string        `yaml:"name"`
	Repository                 string        `yaml:"repository"`
	IconPath                   string        `yaml:"icon_path"`
	Enabled                    bool          `yaml:"enabled"`
	RetainVersions             int           `yaml:"retain_versions"`
	IncludePrerelease          bool          `yaml:"include_prerelease"`
	DownloadMultiplier         int           `yaml:"download_multiplier"`
	AssetInclude               AssetRules    `yaml:"asset_include"`
	AssetExclude               AssetRules    `yaml:"asset_exclude"`
	AssetPipeline              AssetPipeline `yaml:"asset_pipeline"`
	ArchitectureMatchEnabled   bool          `yaml:"architecture_match_enabled"`
	ArchitectureRegex          string        `yaml:"architecture_regex"`
	ArchitectureDefaultEnabled bool          `yaml:"-" json:"-"`
	SystemMatchEnabled         bool          `yaml:"system_match_enabled"`
	SystemRegex                string        `yaml:"system_regex"`
	ResolvedIconPath           string        `yaml:"-"`
	ResolvedClassifyScriptPath string        `yaml:"-"`
}

type AssetRule struct {
	Pattern  string `yaml:"pattern"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
}

type AssetRules []AssetRule

func (p *Project) UnmarshalYAML(value *yaml.Node) error {
	type projectYAML struct {
		ID                         string        `yaml:"id"`
		Name                       string        `yaml:"name"`
		Repository                 string        `yaml:"repository"`
		IconPath                   string        `yaml:"icon_path"`
		Enabled                    bool          `yaml:"enabled"`
		RetainVersions             int           `yaml:"retain_versions"`
		IncludePrerelease          bool          `yaml:"include_prerelease"`
		DownloadMultiplier         int           `yaml:"download_multiplier"`
		AssetInclude               AssetRules    `yaml:"asset_include"`
		AssetExclude               AssetRules    `yaml:"asset_exclude"`
		AssetPipeline              AssetPipeline `yaml:"asset_pipeline"`
		ArchitectureMatchEnabled   bool          `yaml:"architecture_match_enabled"`
		ArchitectureRegex          string        `yaml:"architecture_regex"`
		ArchitectureDefaultEnabled bool          `yaml:"architecture_default_enabled"`
		SystemMatchEnabled         bool          `yaml:"system_match_enabled"`
		SystemRegex                string        `yaml:"system_regex"`
	}
	var raw projectYAML
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = Project{
		ID: raw.ID, Name: raw.Name, Repository: raw.Repository,
		IconPath: raw.IconPath, Enabled: raw.Enabled,
		RetainVersions:     raw.RetainVersions,
		IncludePrerelease:  raw.IncludePrerelease,
		DownloadMultiplier: raw.DownloadMultiplier,
		AssetInclude:       raw.AssetInclude, AssetExclude: raw.AssetExclude,
		AssetPipeline:              raw.AssetPipeline,
		ArchitectureMatchEnabled:   raw.ArchitectureMatchEnabled,
		ArchitectureRegex:          raw.ArchitectureRegex,
		ArchitectureDefaultEnabled: raw.ArchitectureDefaultEnabled,
		SystemMatchEnabled:         raw.SystemMatchEnabled,
		SystemRegex:                raw.SystemRegex,
	}
	return nil
}

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
	legacyArchitectureDefault := false
	data, repaired, err := readYAMLWithRepair(path, &c, ProjectsExample, ProjectsRepairExample,
		func(doc *yaml.Node) bool {
			changed, found := migrateProjectsArchitectureDefault(doc)
			legacyArchitectureDefault = legacyArchitectureDefault || found
			return changed
		})
	if err != nil {
		return c, err
	}
	if legacyArchitectureDefault {
		warnDeprecated(warn, "projects[].architecture_default_enabled",
			"projects[].architecture_match_enabled")
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
		p.ResolvedClassifyScriptPath = resolveOptionalProjectPath(baseDir, p.AssetPipeline.Classify.Script.Path)
		if err := validateProject(*p, known); err != nil {
			return c, err
		}
		known[p.ID] = true
	}
	return c, writeRepairedYAML(path, data, repaired)
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
	if err := validateAssetPipeline(p.ID, p.AssetPipeline); err != nil {
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
