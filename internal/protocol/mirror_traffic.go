package protocol

import (
	"math"
	"time"
)

// MirrorTraffic measures application payload written to download responses.
// Host interface traffic and inbound synchronization bytes are separate metrics.
type MirrorTraffic struct {
	SampledAt          time.Time `json:"sampled_at"`
	WindowSeconds      float64   `json:"window_seconds"`
	PublicBandwidthBPS int64     `json:"public_bandwidth_bps"`
	SwarmBandwidthBPS  int64     `json:"swarm_bandwidth_bps"`
}

func (p *MirrorTraffic) Valid() bool {
	return p == nil || (p.WindowSeconds >= 0 && !math.IsNaN(p.WindowSeconds) &&
		!math.IsInf(p.WindowSeconds, 0) && p.PublicBandwidthBPS >= 0 && p.SwarmBandwidthBPS >= 0)
}
