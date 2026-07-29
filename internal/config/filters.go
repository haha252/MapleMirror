package config

import (
	"fmt"
	"regexp"
	"strings"
)

const DefaultFilterCacheMaxSize = "8 MiB"

var filterIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Filters struct {
	Cache      FilterCache      `yaml:"cache" json:"cache"`
	Selectors  []FilterSelector `yaml:"selectors" json:"filter_groups"`
	CacheBytes int64            `yaml:"-" json:"-"`
}

type FilterCache struct {
	MaxSize string `yaml:"max_size" json:"max_size"`
}

type FilterSelector struct {
	ID      string         `yaml:"id" json:"id"`
	Name    string         `yaml:"name" json:"name"`
	Options []FilterOption `yaml:"options" json:"options"`
}

type FilterOption struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

func LoadFilters(path string, warn WarnFunc) (Filters, error) {
	var filters Filters
	data, repaired, err := readYAMLWithRepair(path, &filters, FiltersExample, FiltersExample)
	if err != nil {
		return filters, err
	}
	if strings.TrimSpace(filters.Cache.MaxSize) == "" {
		filters.Cache.MaxSize = DefaultFilterCacheMaxSize
		warnDefault(warn, "filters.cache.max_size", DefaultFilterCacheMaxSize)
	}
	if err := validateFilters(&filters); err != nil {
		return filters, err
	}
	return filters, writeRepairedYAML(path, data, repaired)
}

func validateFilters(filters *Filters) error {
	size, err := ParseBytes("filters.cache.max_size", filters.Cache.MaxSize, true)
	if err != nil {
		return err
	}
	filters.CacheBytes = size
	selectorIDs := map[string]bool{}
	for i := range filters.Selectors {
		selector := &filters.Selectors[i]
		selector.ID = strings.TrimSpace(selector.ID)
		selector.Name = strings.TrimSpace(selector.Name)
		if !filterIDPattern.MatchString(selector.ID) {
			return fmt.Errorf("配置字段 selectors[%d].id 必须使用小写字母、数字、短横线或下划线", i)
		}
		if selectorIDs[selector.ID] {
			return fmt.Errorf("配置字段 selectors[%d].id 不得重复", i)
		}
		if selector.Name == "" {
			return fmt.Errorf("配置字段 selectors[%d].name 不能为空", i)
		}
		selectorIDs[selector.ID] = true
		optionIDs := map[string]bool{}
		for j := range selector.Options {
			option := &selector.Options[j]
			option.ID = strings.TrimSpace(option.ID)
			option.Name = strings.TrimSpace(option.Name)
			if !filterIDPattern.MatchString(option.ID) {
				return fmt.Errorf("配置字段 selectors[%d].options[%d].id 必须使用小写字母、数字、短横线或下划线", i, j)
			}
			if optionIDs[option.ID] {
				return fmt.Errorf("配置字段 selectors[%d].options[%d].id 不得重复", i, j)
			}
			if option.Name == "" {
				return fmt.Errorf("配置字段 selectors[%d].options[%d].name 不能为空", i, j)
			}
			optionIDs[option.ID] = true
		}
	}
	return nil
}
