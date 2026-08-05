package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const VDFIterationsHardLimit uint64 = 100_000_000

type VDFSizeTier struct {
	MinSize    string `yaml:"min_size"`
	Iterations uint64 `yaml:"iterations"`
}

type VDF struct {
	ChallengeTTL         string `yaml:"challenge_ttl"`
	KeyRotationInterval  string `yaml:"key_rotation_interval"`
	ElevatedMultiplier   int    `yaml:"elevated_multiplier"`
	SevereMultiplier     int    `yaml:"severe_multiplier"`
	MaxIterations        uint64 `yaml:"max_iterations"`
	MaxParallelCreations int    `yaml:"max_parallel_creations"`
}

var defaultVDFSizeTiers = []VDFSizeTier{
	{MinSize: "0 B", Iterations: 12000},
	{MinSize: "5 MiB", Iterations: 48000},
	{MinSize: "50 MiB", Iterations: 96000},
	{MinSize: "150 MiB", Iterations: 192000},
	{MinSize: "300 MiB", Iterations: 384000},
	{MinSize: "500 MiB", Iterations: 768000},
}

func applyVDFDefaults(c *Master, warn WarnFunc) {
	setString(&c.VDF.ChallengeTTL, "2m", "vdf.challenge_ttl", warn)
	setString(&c.VDF.KeyRotationInterval, "24h", "vdf.key_rotation_interval", warn)
	if c.VDF.ElevatedMultiplier == 0 {
		c.VDF.ElevatedMultiplier = 2
		warnDefault(warn, "vdf.elevated_multiplier", "2")
	}
	if c.VDF.SevereMultiplier == 0 {
		c.VDF.SevereMultiplier = 4
		warnDefault(warn, "vdf.severe_multiplier", "4")
	}
	if c.VDF.MaxIterations == 0 {
		c.VDF.MaxIterations = 10_000_000
		warnDefault(warn, "vdf.max_iterations", "10000000")
	}
	if c.VDF.MaxParallelCreations == 0 {
		c.VDF.MaxParallelCreations = 4
		warnDefault(warn, "vdf.max_parallel_creations", "4")
	}
}

func validateVDF(c Master) error {
	if err := validateVDFSizeTiers(c.VDFSizeTiers); err != nil {
		return err
	}
	if c.VDF.MaxIterations == 0 || c.VDF.MaxIterations > VDFIterationsHardLimit {
		return errors.New("vdf.max_iterations 超出有效范围")
	}
	if !(1 < c.VDF.ElevatedMultiplier && c.VDF.ElevatedMultiplier < c.VDF.SevereMultiplier) {
		return errors.New("vdf 倍率必须满足 1 < elevated_multiplier < severe_multiplier")
	}
	if c.VDF.MaxParallelCreations <= 0 {
		return errors.New("vdf.max_parallel_creations 必须大于零")
	}
	ttl, _ := time.ParseDuration(c.VDF.ChallengeTTL)
	rotation, _ := time.ParseDuration(c.VDF.KeyRotationInterval)
	if rotation <= ttl {
		return errors.New("vdf.key_rotation_interval 必须大于 vdf.challenge_ttl")
	}
	return nil
}

func validateVDFSizeTiers(tiers []VDFSizeTier) error {
	if len(tiers) == 0 {
		return errors.New("vdf_size_tiers 至少需要一个分档")
	}
	previous := int64(-1)
	for index, tier := range tiers {
		value, err := ParseBytes(fmt.Sprintf("vdf_size_tiers[%d].min_size", index), tier.MinSize, true)
		if err != nil {
			return err
		}
		if index == 0 && value != 0 {
			return errors.New("vdf_size_tiers 第一档 min_size 必须为 0 B")
		}
		if value <= previous {
			return errors.New("vdf_size_tiers 的 min_size 必须严格递增")
		}
		if tier.Iterations == 0 || tier.Iterations > VDFIterationsHardLimit {
			return fmt.Errorf("配置字段 vdf_size_tiers[%d].iterations 超出有效范围", index)
		}
		previous = value
	}
	return nil
}

func LoadVDFSizeTiers(path string) ([]VDFSizeTier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取主节点配置失败：%w", err)
	}
	var partial struct {
		VDFSizeTiers []VDFSizeTier `yaml:"vdf_size_tiers"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return nil, fmt.Errorf("解析 YAML 配置失败：%w", err)
	}
	if err := validateVDFSizeTiers(partial.VDFSizeTiers); err != nil {
		return nil, err
	}
	return append([]VDFSizeTier(nil), partial.VDFSizeTiers...), nil
}

func migrateVDFChallengeTTL(doc *yaml.Node) (bool, bool) {
	root := yamlRoot(doc)
	changed := ensureVDFSizeTiers(root)
	altcha := findYAMLMapValue(root, "altcha")
	if altcha == nil || altcha.Kind != yaml.MappingNode {
		return changed, false
	}
	old := findYAMLMapValue(altcha, "challenge_ttl")
	if old == nil {
		return changed, false
	}
	vdf := yamlEnsureMap(root, "vdf", "RSA repeated-squaring 挑战配置。")
	if vdf != nil && findYAMLMapValue(vdf, "challenge_ttl") == nil {
		vdf.Content = append(vdf.Content, yamlStringKey("challenge_ttl", "V2 挑战有效时间。"), cloneYAMLNode(old))
	}
	yamlRemoveMapKey(altcha, "challenge_ttl")
	return true, true
}

func ensureVDFSizeTiers(root *yaml.Node) bool {
	tiers := findYAMLMapValue(root, "vdf_size_tiers")
	if tiers == nil {
		tiers = yamlEnsureSequence(root, "vdf_size_tiers", "RSA repeated-squaring 迭代数分档。")
	}
	if tiers == nil || tiers.Kind != yaml.SequenceNode || len(tiers.Content) > 0 {
		return false
	}
	for _, tier := range defaultVDFSizeTiers {
		tiers.Content = append(tiers.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			yamlStringKey("min_size", "该档生效的最小资产大小。"),
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: tier.MinSize},
			yamlStringKey("iterations", "该档的基础重复平方迭代数。"),
			{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", tier.Iterations)},
		}})
	}
	return true
}
