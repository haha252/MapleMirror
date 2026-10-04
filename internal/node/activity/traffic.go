package activity

import (
	"time"

	"mirror-server/internal/protocol"
)

func (c *Counters) RecordPublicBytes(n int64) {
	if c != nil && n > 0 {
		c.publicBytes.Add(n)
	}
}

func (c *Counters) RecordSwarmBytes(n int64) {
	if c != nil && n > 0 {
		c.swarmBytes.Add(n)
	}
}

func (c *Counters) SampleTraffic() *protocol.MirrorTraffic {
	return c.sampleTrafficAt(time.Now())
}

func (c *Counters) sampleTrafficAt(now time.Time) *protocol.MirrorTraffic {
	if c == nil {
		return nil
	}
	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()
	if !c.trafficAt.IsZero() && now.Sub(c.trafficAt) >= 0 && now.Sub(c.trafficAt) < time.Second {
		out := c.traffic
		return &out
	}
	public, swarm := c.publicBytes.Load(), c.swarmBytes.Load()
	out := protocol.MirrorTraffic{SampledAt: now.UTC()}
	if !c.trafficAt.IsZero() && now.After(c.trafficAt) {
		out.WindowSeconds = now.Sub(c.trafficAt).Seconds()
		out.PublicBandwidthBPS = int64(float64(public-c.previousPublic) / out.WindowSeconds)
		out.SwarmBandwidthBPS = int64(float64(swarm-c.previousSwarm) / out.WindowSeconds)
	}
	c.trafficAt, c.previousPublic, c.previousSwarm = now, public, swarm
	c.traffic = out
	return &out
}
