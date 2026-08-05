package public

import (
	"fmt"

	"mirror-server/internal/config"
)

type vdfSizeTier struct {
	minBytes   int64
	iterations uint64
}

type vdfPolicy struct {
	tiers              []vdfSizeTier
	elevatedMultiplier int
	severeMultiplier   int
	maxIterations      uint64
}

func newVDFPolicy(raw []config.VDFSizeTier, cfg config.VDF) (vdfPolicy, error) {
	if len(raw) == 0 {
		return vdfPolicy{}, fmt.Errorf("vdf_size_tiers 至少需要一个分档")
	}
	policy := vdfPolicy{tiers: make([]vdfSizeTier, 0, len(raw)),
		elevatedMultiplier: cfg.ElevatedMultiplier, severeMultiplier: cfg.SevereMultiplier,
		maxIterations: cfg.MaxIterations}
	previous := int64(-1)
	for index, tier := range raw {
		minBytes, err := config.ParseBytes(fmt.Sprintf("vdf_size_tiers[%d].min_size", index), tier.MinSize, true)
		if err != nil {
			return vdfPolicy{}, err
		}
		if index == 0 && minBytes != 0 {
			return vdfPolicy{}, fmt.Errorf("vdf_size_tiers 第一档必须从 0 B 开始")
		}
		if minBytes <= previous || tier.Iterations == 0 {
			return vdfPolicy{}, fmt.Errorf("vdf_size_tiers 分档无效")
		}
		policy.tiers = append(policy.tiers, vdfSizeTier{minBytes: minBytes, iterations: tier.Iterations})
		previous = minBytes
	}
	return policy, nil
}

func (p vdfPolicy) parameters(sizeBytes int64, level abuseLevel) (uint64, int) {
	base, multiplier := p.tiers[0].iterations, 1
	for _, tier := range p.tiers[1:] {
		if sizeBytes < tier.minBytes {
			break
		}
		base = tier.iterations
	}
	if level == abuseLevelElevated {
		multiplier = p.elevatedMultiplier
	}
	if level == abuseLevelSevere {
		multiplier = p.severeMultiplier
	}
	iterations := p.maxIterations
	if multiplier > 0 && base <= p.maxIterations/uint64(multiplier) {
		iterations = base * uint64(multiplier)
	}
	if iterations > p.maxIterations {
		iterations = p.maxIterations
	}
	return iterations, multiplier
}

func (s *vdfService) currentPolicy() vdfPolicy {
	s.policyMu.RLock()
	defer s.policyMu.RUnlock()
	return s.policy
}

func (s *vdfService) setPolicy(policy vdfPolicy) {
	s.policyMu.Lock()
	s.policy = policy
	s.policyMu.Unlock()
}

func (s *vdfService) parameters(sizeBytes int64, level abuseLevel) (uint64, int) {
	return s.currentPolicy().parameters(sizeBytes, level)
}

func equalVDFPolicies(left, right vdfPolicy) bool {
	if left.elevatedMultiplier != right.elevatedMultiplier ||
		left.severeMultiplier != right.severeMultiplier ||
		left.maxIterations != right.maxIterations || len(left.tiers) != len(right.tiers) {
		return false
	}
	for index, tier := range left.tiers {
		if tier != right.tiers[index] {
			return false
		}
	}
	return true
}
