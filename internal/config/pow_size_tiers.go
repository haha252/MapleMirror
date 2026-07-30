package config

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

type PoWSizeTier struct {
	MinSize    string `yaml:"min_size"`
	Difficulty int    `yaml:"difficulty"`
}

var defaultPoWSizeTiers = []PoWSizeTier{
	{MinSize: "0 B", Difficulty: 20},
	{MinSize: "5 MiB", Difficulty: 22},
	{MinSize: "50 MiB", Difficulty: 23},
	{MinSize: "150 MiB", Difficulty: 24},
	{MinSize: "300 MiB", Difficulty: 25},
	{MinSize: "500 MiB", Difficulty: 26},
}

func DefaultPoWSizeTiers() []PoWSizeTier {
	return append([]PoWSizeTier(nil), defaultPoWSizeTiers...)
}

func readMasterYAML(path string, target *Master) ([]byte, bool, bool, bool, error) {
	var legacyALTCHA, legacyAPI bool
	data, repaired, err := readYAMLWithRepair(path, target, MasterExample, MasterExample,
		func(doc *yaml.Node) bool {
			changed, oldALTCHA, oldAPI := migrateMasterPoWSizeTiers(doc)
			legacyALTCHA = legacyALTCHA || oldALTCHA
			legacyAPI = legacyAPI || oldAPI
			return changed
		})
	return data, repaired, legacyALTCHA, legacyAPI, err
}

func validatePoWSizeTiers(tiers []PoWSizeTier) error {
	if len(tiers) == 0 {
		return errors.New("pow_size_tiers 至少需要一个分档")
	}
	var previous int64 = -1
	for index, tier := range tiers {
		field := fmt.Sprintf("pow_size_tiers[%d].min_size", index)
		minBytes, err := ParseBytes(field, tier.MinSize, true)
		if err != nil {
			return err
		}
		if index == 0 && minBytes != 0 {
			return errors.New("pow_size_tiers 第一档 min_size 必须为 0 B")
		}
		if minBytes <= previous {
			return errors.New("pow_size_tiers 的 min_size 必须严格递增")
		}
		if tier.Difficulty <= 0 {
			return fmt.Errorf("配置字段 pow_size_tiers[%d].difficulty 必须大于零", index)
		}
		previous = minBytes
	}
	return nil
}

func migrateMasterPoWSizeTiers(doc *yaml.Node) (bool, bool, bool) {
	root := yamlRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return false, false, false
	}
	changed := false
	oldALTCHA := removeNestedYAMLKey(root, "altcha", "difficulty")
	oldAPI := removeNestedYAMLKey(root, "api_pow", "leading_zero_bits")
	changed = oldALTCHA || oldAPI
	tiers := findYAMLMapValue(root, "pow_size_tiers")
	if tiers == nil {
		tiers = yamlEnsureSequence(root, "pow_size_tiers",
			"普通网页与公开 API PoW 按可信资产大小共用的基础难度分档。")
		changed = true
	}
	if tiers != nil && tiers.Kind == yaml.SequenceNode && len(tiers.Content) == 0 {
		for _, tier := range defaultPoWSizeTiers {
			tiers.Content = append(tiers.Content, powSizeTierNode(tier))
		}
		changed = true
	}
	return changed, oldALTCHA, oldAPI
}

func removeNestedYAMLKey(root *yaml.Node, parentKey, childKey string) bool {
	parent := findYAMLMapValue(root, parentKey)
	return parent != nil && yamlRemoveMapKey(parent, childKey)
}

func powSizeTierNode(tier PoWSizeTier) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			yamlStringKey("min_size", "该档生效的最小资产大小。"),
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: tier.MinSize},
			yamlStringKey("difficulty", "该档普通 PoW 的 SHA-256 前导零位数。"),
			{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", tier.Difficulty)},
		},
	}
}
