package config

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

func validateProject(p Project, known map[string]bool) error {
	if p.ID == "" || p.Name == "" || known[p.ID] {
		return errors.New("项目 id 和中文名称必须存在，且 id 不得重复")
	}
	if err := validateProjectHomepage(p.HomepageURL); err != nil {
		return fmt.Errorf("项目 %s 的 homepage_url 无效：%w", p.ID, err)
	}
	parts := strings.Split(p.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(p.Repository, "://") {
		return errors.New("项目 repository 必须使用公开 GitHub 仓库的 owner/repo 格式")
	}
	if p.RetainVersions <= 0 || p.DownloadMultiplier <= 0 {
		return errors.New("项目保留版本数和下载倍率必须大于零")
	}
	if !validProjectSelectionMode(p.DefaultSelectionMode) {
		return fmt.Errorf("项目 %s 的 default_selection_mode 必须为 selectors 或 file", p.ID)
	}
	if p.RegexClassificationEnabled() && p.ClassifyArchitectureEnabled() {
		if strings.TrimSpace(p.ClassifyArchitectureRegex()) == "" {
			return fmt.Errorf("项目 %s 启用架构匹配时必须配置 architecture_regex", p.ID)
		}
		if _, err := regexp.Compile(p.ClassifyArchitectureRegex()); err != nil {
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
	if p.RegexClassificationEnabled() && p.ClassifySystemEnabled() {
		if strings.TrimSpace(p.ClassifySystemRegex()) == "" {
			return fmt.Errorf("项目 %s 启用系统匹配时必须配置 system_regex", p.ID)
		}
		if _, err := regexp.Compile(p.ClassifySystemRegex()); err != nil {
			return fmt.Errorf("项目 %s 的系统提取正则无效：%w", p.ID, err)
		}
	}
	return nil
}

func validateProjectHomepage(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return errors.New("必须是 http 或 https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("必须是 http 或 https URL")
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
