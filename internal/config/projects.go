package config

import (
	"errors"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Projects struct {
	ProjectFiles []string  `yaml:"project_files,omitempty"`
	Projects     []Project `yaml:"projects,omitempty"`
}

type Project struct {
	ID                         string        `yaml:"id"`
	Name                       string        `yaml:"name"`
	Repository                 string        `yaml:"repository"`
	Description                string        `yaml:"description"`
	HomepageURL                string        `yaml:"homepage_url"`
	IconPath                   string        `yaml:"icon_path"`
	Enabled                    bool          `yaml:"enabled"`
	RetainVersions             int           `yaml:"retain_versions"`
	IncludePrerelease          bool          `yaml:"include_prerelease"`
	DownloadMultiplier         int           `yaml:"download_multiplier"`
	DefaultSelectionMode       string        `yaml:"default_selection_mode"`
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
		Description                string        `yaml:"description"`
		HomepageURL                string        `yaml:"homepage_url"`
		IconPath                   string        `yaml:"icon_path"`
		Enabled                    bool          `yaml:"enabled"`
		RetainVersions             int           `yaml:"retain_versions"`
		IncludePrerelease          bool          `yaml:"include_prerelease"`
		DownloadMultiplier         int           `yaml:"download_multiplier"`
		DefaultSelectionMode       string        `yaml:"default_selection_mode"`
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
		Description: raw.Description, HomepageURL: raw.HomepageURL,
		IconPath: raw.IconPath, Enabled: raw.Enabled,
		RetainVersions:       raw.RetainVersions,
		IncludePrerelease:    raw.IncludePrerelease,
		DownloadMultiplier:   raw.DownloadMultiplier,
		DefaultSelectionMode: raw.DefaultSelectionMode,
		AssetInclude:         raw.AssetInclude, AssetExclude: raw.AssetExclude,
		AssetPipeline:              raw.AssetPipeline,
		ArchitectureMatchEnabled:   raw.ArchitectureMatchEnabled,
		ArchitectureRegex:          raw.ArchitectureRegex,
		ArchitectureDefaultEnabled: raw.ArchitectureDefaultEnabled,
		SystemMatchEnabled:         raw.SystemMatchEnabled,
		SystemRegex:                raw.SystemRegex,
	}
	if raw.AssetPipeline.Classify.hasRegexConfig() {
		p.ArchitectureMatchEnabled = raw.AssetPipeline.Classify.Regex.ArchitectureMatchEnabled
		p.ArchitectureRegex = raw.AssetPipeline.Classify.Regex.ArchitectureRegex
		p.SystemMatchEnabled = raw.AssetPipeline.Classify.Regex.SystemMatchEnabled
		p.SystemRegex = raw.AssetPipeline.Classify.Regex.SystemRegex
	}
	if raw.ArchitectureMatchEnabled || strings.TrimSpace(raw.ArchitectureRegex) != "" {
		p.AssetPipeline.Classify.Regex.ArchitectureMatchEnabled = raw.ArchitectureMatchEnabled
		p.AssetPipeline.Classify.Regex.ArchitectureRegex = raw.ArchitectureRegex
	}
	if raw.SystemMatchEnabled || strings.TrimSpace(raw.SystemRegex) != "" {
		p.AssetPipeline.Classify.Regex.SystemMatchEnabled = raw.SystemMatchEnabled
		p.AssetPipeline.Classify.Regex.SystemRegex = raw.SystemRegex
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
			if migrateProjectsLegacyRegex(doc) {
				changed = true
			}
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
		if err := prepareProject(p, baseDir, known, warn); err != nil {
			return c, err
		}
		known[p.ID] = true
	}
	refs, err := ResolveProjectFileReferences(path, c.ProjectFiles)
	if err != nil {
		return c, err
	}
	for _, ref := range refs {
		project, err := LoadProjectFile(ref.Path, warn)
		if err != nil {
			return c, err
		}
		if err := prepareProject(&project, filepath.Dir(ref.Path), known, warn); err != nil {
			return c, err
		}
		known[project.ID] = true
		c.Projects = append(c.Projects, project)
	}
	return c, writeRepairedYAML(path, data, repaired)
}
