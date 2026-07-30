package public

import (
	"fmt"

	"mirror-server/internal/config"
)

type powSizeTier struct {
	minBytes   int64
	difficulty int
}

type powSizePolicy struct {
	tiers   []powSizeTier
	maxBits int
}

func newPoWSizePolicy(raw []config.PoWSizeTier, maxBits int) (powSizePolicy, error) {
	if len(raw) == 0 {
		return powSizePolicy{}, fmt.Errorf("pow_size_tiers 至少需要一个分档")
	}
	if maxBits <= 0 {
		return powSizePolicy{}, fmt.Errorf("abuse_control.challenge.max_bits 必须大于零")
	}
	policy := powSizePolicy{
		tiers:   make([]powSizeTier, 0, len(raw)),
		maxBits: maxBits,
	}
	var previous int64 = -1
	for index, tier := range raw {
		if tier.Difficulty <= 0 {
			return powSizePolicy{}, fmt.Errorf(
				"pow_size_tiers[%d].difficulty 必须大于零", index)
		}
		if tier.Difficulty > maxBits {
			return powSizePolicy{}, fmt.Errorf(
				"pow_size_tiers[%d].difficulty=%d 超过普通 PoW 上限 abuse_control.challenge.max_bits=%d",
				index, tier.Difficulty, maxBits)
		}
		minBytes, err := config.ParseBytes(
			fmt.Sprintf("pow_size_tiers[%d].min_size", index), tier.MinSize, true)
		if err != nil {
			return powSizePolicy{}, err
		}
		if index == 0 && minBytes != 0 {
			return powSizePolicy{}, fmt.Errorf(
				"pow_size_tiers 第一档 min_size 必须为 0 B")
		}
		if minBytes <= previous {
			return powSizePolicy{}, fmt.Errorf(
				"pow_size_tiers 的 min_size 必须严格递增")
		}
		policy.tiers = append(policy.tiers, powSizeTier{
			minBytes: minBytes, difficulty: tier.Difficulty,
		})
		previous = minBytes
	}
	return policy, nil
}

func (p powSizePolicy) difficulty(sizeBytes int64, additionalBits int) int {
	difficulty := p.tiers[0].difficulty
	for _, tier := range p.tiers[1:] {
		if sizeBytes < tier.minBytes {
			break
		}
		difficulty = tier.difficulty
	}
	if additionalBits > 0 {
		difficulty += additionalBits
	}
	if difficulty > p.maxBits {
		return p.maxBits
	}
	return difficulty
}
