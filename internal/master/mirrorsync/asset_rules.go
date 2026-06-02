package mirrorsync

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"mirror-server/internal/config"
)

func assetAllowed(name string, includes, excludes config.AssetRules) (bool, string, error) {
	excluded, err := assetRulesExclude(name, excludes)
	if err != nil {
		return false, "", err
	}
	if excluded {
		return false, "asset exclude match", nil
	}
	included, err := assetRulesInclude(name, includes)
	if err != nil {
		return false, "", err
	}
	if !included {
		return false, "asset include mismatch", nil
	}
	return true, "", nil
}

func assetRulesInclude(name string, rules config.AssetRules) (bool, error) {
	hasOptional := false
	optionalMatched := false
	for _, rule := range rules {
		matched, err := matchAssetRule(name, rule)
		if err != nil {
			return false, err
		}
		if rule.Required {
			if !matched {
				return false, nil
			}
			continue
		}
		hasOptional = true
		optionalMatched = optionalMatched || matched
	}
	return !hasOptional || optionalMatched, nil
}

func assetRulesExclude(name string, rules config.AssetRules) (bool, error) {
	hasRequired := false
	allRequiredMatched := true
	for _, rule := range rules {
		matched, err := matchAssetRule(name, rule)
		if err != nil {
			return false, err
		}
		if rule.Required {
			hasRequired = true
			allRequiredMatched = allRequiredMatched && matched
			continue
		}
		if matched {
			return true, nil
		}
	}
	return hasRequired && allRequiredMatched, nil
}

func matchAssetRule(name string, rule config.AssetRule) (bool, error) {
	switch strings.TrimSpace(rule.Type) {
	case "", "glob":
		return path.Match(rule.Pattern, name)
	case "regex":
		return regexp.MatchString(rule.Pattern, name)
	default:
		return false, fmt.Errorf("资产匹配规则 type 必须为 glob 或 regex：%s", rule.Type)
	}
}

func assetRulesHash(rules config.AssetRules) string {
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleType := rule.Type
		if ruleType == "" {
			ruleType = "glob"
		}
		parts = append(parts, strings.Join([]string{
			rule.Pattern,
			ruleType,
			fmt.Sprint(rule.Required),
		}, ","))
	}
	return strings.Join(parts, ";")
}
