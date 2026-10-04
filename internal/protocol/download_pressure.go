package protocol

import (
	"math"
	"time"
)

const DownloadPressureMaxAge = 2 * time.Minute

// DownloadPressure describes observed capacity, not the cause of a bottleneck.
// All bandwidth fields are bytes/second, consistent with the existing protocol.
type DownloadPressure struct {
	SampledAt             time.Time `json:"sampled_at"`
	WindowSeconds         float64   `json:"window_seconds"`
	ObservedPeers         int       `json:"observed_peers"`
	ConstrainedPeers      int       `json:"constrained_peers"`
	DeliveryBandwidthBPS  int64     `json:"delivery_bandwidth_bps"`
	EffectiveBandwidthBPS int64     `json:"effective_bandwidth_bps"`
	LimitedAt             time.Time `json:"limited_at"`
}

func (p *DownloadPressure) Limited(now time.Time) bool {
	return p != nil && p.Valid() && p.EffectiveBandwidthBPS > 0 && !p.LimitedAt.IsZero() &&
		!p.LimitedAt.After(now.Add(5*time.Second)) && now.Sub(p.LimitedAt) <= DownloadPressureMaxAge
}

func (p *DownloadPressure) Valid() bool {
	return p == nil || (p.WindowSeconds >= 0 && !math.IsNaN(p.WindowSeconds) && !math.IsInf(p.WindowSeconds, 0) &&
		p.ObservedPeers >= 0 && p.ConstrainedPeers >= 0 && p.ConstrainedPeers <= p.ObservedPeers &&
		p.DeliveryBandwidthBPS >= 0 && p.EffectiveBandwidthBPS >= 0)
}

// RoutingRatio retains a pressure floor during the brief recovery cooldown.
// Otherwise lowering traffic would immediately make a constrained node look idle.
func (p *DownloadPressure) RoutingRatio(actual, target int64, now time.Time) float64 {
	if target <= 0 {
		return 0
	}
	ratio := float64(actual) / float64(target)
	if p.Limited(now) {
		capacity := min(target, p.EffectiveBandwidthBPS)
		ratio = max(ratio, float64(actual)/float64(capacity), 0.9)
	}
	return ratio
}
