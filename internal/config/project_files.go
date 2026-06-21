package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ProjectFileReference struct {
	Pattern  string
	Path     string
	FromGlob bool
}

func ResolveProjectFileReferences(projectsPath string, projectFiles []string) ([]ProjectFileReference, error) {
	baseDir := filepath.Dir(projectsPath)
	byPath := map[string]ProjectFileReference{}
	for _, entry := range projectFiles {
		clean, hasGlob, err := cleanProjectFileEntry(entry)
		if err != nil {
			return nil, err
		}
		pattern := filepath.Join(baseDir, clean)
		if !hasGlob {
			byPath[pattern] = ProjectFileReference{Pattern: clean, Path: pattern}
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("项目文件匹配规则无效：%w", err)
		}
		for _, match := range matches {
			if err := validateProjectFileExtension(match); err != nil {
				return nil, err
			}
			byPath[match] = ProjectFileReference{Pattern: clean, Path: match, FromGlob: true}
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	refs := make([]ProjectFileReference, 0, len(paths))
	for _, path := range paths {
		refs = append(refs, byPath[path])
	}
	return refs, nil
}

func LoadProjectFile(path string, warn WarnFunc) (Project, error) {
	var project Project
	data, err := os.ReadFile(path)
	if err != nil {
		return project, fmt.Errorf("读取项目配置文件失败：%w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return project, fmt.Errorf("解析项目配置文件失败：%w", err)
	}
	changed, found := migrateSingleProjectArchitectureDefault(&doc)
	if migrateSingleProjectLegacyRegex(&doc) {
		changed = true
	}
	if pruneProjectFileUnknownKeys(&doc) {
		changed = true
	}
	if found {
		warnDeprecated(warn, "projects[].architecture_default_enabled",
			"projects[].architecture_match_enabled")
	}
	if changed {
		encoded, err := yaml.Marshal(&doc)
		if err != nil {
			return project, fmt.Errorf("编码项目配置文件失败：%w", err)
		}
		data = encoded
	}
	if err := yaml.Unmarshal(data, &project); err != nil {
		return project, fmt.Errorf("解析项目配置文件失败：%w", err)
	}
	if changed {
		if err := replaceConfigFile(path, data); err != nil {
			return project, err
		}
	}
	return project, nil
}

func pruneProjectFileUnknownKeys(doc *yaml.Node) bool {
	var template yaml.Node
	if err := yaml.Unmarshal(ProjectExample, &template); err != nil {
		return false
	}
	return pruneUnknownYAML(doc, &template)
}

func cleanProjectFileEntry(value string) (string, bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false, errors.New("project_files 不能为空路径")
	}
	if filepath.IsAbs(trimmed) || isWindowsAbsolutePath(trimmed) {
		return "", false, errors.New("project_files 必须使用相对路径")
	}
	clean := filepath.Clean(trimmed)
	if clean == "." || clean == "" {
		return "", false, errors.New("project_files 不能为空路径")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false, errors.New("project_files 不得越级访问配置目录")
	}
	hasGlob := strings.ContainsAny(clean, "*?[")
	if err := validateProjectFileExtension(clean); err != nil {
		return "", false, err
	}
	return clean, hasGlob, nil
}

func validateProjectFileExtension(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return nil
	default:
		return errors.New("project_files 只允许 yaml 或 yml 文件")
	}
}

func prepareProject(p *Project, baseDir string, known map[string]bool, warn WarnFunc) error {
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
		return err
	}
	p.ResolvedIconPath = resolved
	p.ResolvedClassifyScriptPath = resolveOptionalProjectPath(baseDir, p.AssetPipeline.Classify.Script.Path)
	return validateProject(*p, known)
}
