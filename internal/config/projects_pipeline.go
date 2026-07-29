package config

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type AssetPipeline struct {
	Selectors AssetSelectorConfig `yaml:"selectors,omitempty"`
	Classify  AssetClassifyConfig `yaml:"classify"`
}

type AssetClassifyConfig struct {
	Mode   string                   `yaml:"mode"`
	Regex  AssetClassifyRegexConfig `yaml:"regex"`
	Rules  []AssetClassifyRule      `yaml:"rules"`
	Script AssetClassifyScript      `yaml:"script"`
}

type AssetClassifyRegexConfig struct {
	ArchitectureMatchEnabled bool   `yaml:"architecture_match_enabled"`
	ArchitectureRegex        string `yaml:"architecture_regex"`
	SystemMatchEnabled       bool   `yaml:"system_match_enabled"`
	SystemRegex              string `yaml:"system_regex"`
}

type AssetClassifyRule struct {
	Match  AssetClassifyMatch  `yaml:"match"`
	Assign AssetClassification `yaml:"assign"`
}

type AssetClassifyMatch struct {
	Exact string `yaml:"exact"`
	Glob  string `yaml:"glob"`
	Regex string `yaml:"regex"`
}

type AssetClassification struct {
	System       string   `yaml:"system,omitempty"`
	Architecture string   `yaml:"architecture,omitempty"`
	Variant      string   `yaml:"variant,omitempty"`
	DisplayLabel string   `yaml:"display_label,omitempty"`
	Priority     *int     `yaml:"priority,omitempty"`
	Labels       []string `yaml:"labels,omitempty"`
}

type AssetClassifyScript struct {
	Path   string `yaml:"path"`
	Inline string `yaml:"inline"`
}

func (p Project) PipelineUsesArchitecture() bool {
	if p.RegexClassificationEnabled() && p.ClassifyArchitectureEnabled() {
		return true
	}
	if !p.RuleClassificationEnabled() {
		return false
	}
	if p.AssetPipeline.Classify.Script.Path != "" || p.AssetPipeline.Classify.Script.Inline != "" {
		return true
	}
	for _, rule := range p.AssetPipeline.Classify.Rules {
		if strings.TrimSpace(rule.Assign.Architecture) != "" {
			return true
		}
	}
	return false
}

func (p Project) PipelineUsesSystem() bool {
	if p.RegexClassificationEnabled() && p.ClassifySystemEnabled() {
		return true
	}
	if !p.RuleClassificationEnabled() {
		return false
	}
	if p.AssetPipeline.Classify.Script.Path != "" || p.AssetPipeline.Classify.Script.Inline != "" {
		return true
	}
	for _, rule := range p.AssetPipeline.Classify.Rules {
		if strings.TrimSpace(rule.Assign.System) != "" {
			return true
		}
	}
	return false
}

func (p Project) RegexClassificationEnabled() bool {
	mode := p.classifyMode()
	return mode == "" || mode == "regex"
}

func (p Project) RuleClassificationEnabled() bool {
	mode := p.classifyMode()
	return mode == "" || mode == "rules"
}

func (p Project) classifyMode() string {
	return strings.ToLower(strings.TrimSpace(p.AssetPipeline.Classify.Mode))
}

func (p Project) ClassifyArchitectureEnabled() bool {
	if p.AssetPipeline.Classify.hasRegexConfig() {
		return p.AssetPipeline.Classify.Regex.ArchitectureMatchEnabled
	}
	return p.ArchitectureMatchEnabled
}

func (p Project) ClassifyArchitectureRegex() string {
	if p.AssetPipeline.Classify.hasRegexConfig() {
		return p.AssetPipeline.Classify.Regex.ArchitectureRegex
	}
	return p.ArchitectureRegex
}

func (p Project) ClassifySystemEnabled() bool {
	if p.AssetPipeline.Classify.hasRegexConfig() {
		return p.AssetPipeline.Classify.Regex.SystemMatchEnabled
	}
	return p.SystemMatchEnabled
}

func (p Project) ClassifySystemRegex() string {
	if p.AssetPipeline.Classify.hasRegexConfig() {
		return p.AssetPipeline.Classify.Regex.SystemRegex
	}
	return p.SystemRegex
}

func (c AssetClassifyConfig) hasRegexConfig() bool {
	return c.Regex.ArchitectureMatchEnabled || c.Regex.ArchitectureRegex != "" ||
		c.Regex.SystemMatchEnabled || c.Regex.SystemRegex != ""
}

func validateAssetPipeline(projectID string, pipeline AssetPipeline) error {
	mode := strings.ToLower(strings.TrimSpace(pipeline.Classify.Mode))
	switch mode {
	case "", "regex", "rules":
	default:
		return fmt.Errorf("项目 %s 的 asset_pipeline.classify.mode 必须为 regex 或 rules", projectID)
	}
	if mode == "regex" {
		return nil
	}
	script := pipeline.Classify.Script
	if strings.TrimSpace(script.Path) != "" && strings.TrimSpace(script.Inline) != "" {
		return fmt.Errorf("项目 %s 的 asset_pipeline.classify.script 只能配置 path 或 inline 之一", projectID)
	}
	if err := validateProjectRelativePath("asset_pipeline.classify.script.path",
		script.Path, map[string]bool{".star": true, ".bzl": true}); err != nil {
		return fmt.Errorf("项目 %s 的 %w", projectID, err)
	}
	for i, rule := range pipeline.Classify.Rules {
		if err := validateClassifyMatch(projectID, i, rule.Match); err != nil {
			return err
		}
		if err := validateClassifyAssign(projectID, i, rule.Assign); err != nil {
			return err
		}
	}
	return nil
}

func validateClassifyMatch(projectID string, index int, match AssetClassifyMatch) error {
	count := 0
	if strings.TrimSpace(match.Exact) != "" {
		count++
	}
	if strings.TrimSpace(match.Glob) != "" {
		count++
		if _, err := path.Match(match.Glob, ""); err != nil {
			return fmt.Errorf("项目 %s 的分类规则 %d glob 无效：%w", projectID, index+1, err)
		}
	}
	if strings.TrimSpace(match.Regex) != "" {
		count++
		if _, err := regexp.Compile(match.Regex); err != nil {
			return fmt.Errorf("项目 %s 的分类规则 %d regex 无效：%w", projectID, index+1, err)
		}
	}
	if count != 1 {
		return fmt.Errorf("项目 %s 的分类规则 %d 必须且只能配置 exact、glob 或 regex 之一", projectID, index+1)
	}
	return nil
}

func validateClassifyAssign(projectID string, index int, assign AssetClassification) error {
	if strings.TrimSpace(assign.System) != "" {
		if _, ok := NormalizeAssetSystem(assign.System); !ok {
			return fmt.Errorf("项目 %s 的分类规则 %d system 无效：%s", projectID, index+1, assign.System)
		}
	}
	if strings.TrimSpace(assign.Architecture) != "" {
		if _, ok := NormalizeAssetArchitecture(assign.Architecture); !ok {
			return fmt.Errorf("项目 %s 的分类规则 %d architecture 无效：%s", projectID, index+1, assign.Architecture)
		}
	}
	return nil
}

func NormalizeAssetSystem(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "win", "windows", "win32", "win64":
		return "win", true
	case "linux":
		return "linux", true
	case "darwin", "mac", "macos", "osx":
		return "darwin", true
	case "none":
		return "None", true
	default:
		return "", false
	}
}

func NormalizeAssetArchitecture(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "amd64", "x64":
		return "amd64", true
	case "x86_64":
		return "x86_64", true
	case "arm64", "aarch64", "armv8":
		return "arm64", true
	case "arm64-v8a":
		return "arm64-v8a", true
	case "arm":
		return "arm", true
	case "armeabi-v7a":
		return "armeabi-v7a", true
	case "x86", "i386", "i686":
		return "x86", true
	case "all", "universal", "any":
		return "all", true
	case "none":
		return "None", true
	default:
		return "", false
	}
}
