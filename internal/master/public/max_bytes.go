package public

import "mirror-server/internal/config"

type maxBytesPolicy struct {
	multiplier int64
}

func newMaxBytesPolicy(quota config.Quota) maxBytesPolicy {
	multiplier := quota.AuthorizationMaxBytesMultiplier
	if multiplier <= 0 {
		multiplier = 2
	}
	return maxBytesPolicy{multiplier: int64(multiplier)}
}

func (s *Store) maxBytesPolicy() maxBytesPolicy {
	if s.MaxBytes.multiplier <= 0 {
		s.MaxBytes = newMaxBytesPolicy(config.Quota{AuthorizationMaxBytesMultiplier: 2})
	}
	return s.MaxBytes
}

func (p maxBytesPolicy) maxBytes(assetSize int64) int64 {
	if p.multiplier <= 0 {
		p.multiplier = 2
	}
	return assetSize * p.multiplier
}
