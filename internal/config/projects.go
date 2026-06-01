package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type Projects struct {
	Projects []Project `yaml:"projects"`
}

type Project struct {
	ID                 string   `yaml:"id"`
	Name               string   `yaml:"name"`
	Repository         string   `yaml:"repository"`
	IconPath           string   `yaml:"icon_path"`
	Enabled            bool     `yaml:"enabled"`
	RetainVersions     int      `yaml:"retain_versions"`
	IncludePrerelease  bool     `yaml:"include_prerelease"`
	DownloadMultiplier int      `yaml:"download_multiplier"`
	AssetInclude       []string `yaml:"asset_include"`
	AssetExclude       []string `yaml:"asset_exclude"`
	ArchitectureRegex  string   `yaml:"architecture_regex"`
	SystemMatchEnabled bool     `yaml:"system_match_enabled"`
	SystemRegex        string   `yaml:"system_regex"`
	ResolvedIconPath   string   `yaml:"-"`
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
	if _, err := regexp.Compile(p.ArchitectureRegex); err != nil {
		return fmt.Errorf("项目 %s 的架构提取正则无效：%w", p.ID, err)
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
