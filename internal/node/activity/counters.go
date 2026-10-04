package activity

import (
	"sync"
	"sync/atomic"
	"time"

	"mirror-server/internal/node/networkpressure"
	"mirror-server/internal/protocol"
)

type Counters struct {
	Network         networkpressure.Tracker
	publicDownloads atomic.Int64
	swarmUploads    atomic.Int64
	publicBytes     atomic.Int64
	swarmBytes      atomic.Int64
	trafficMu       sync.Mutex
	trafficAt       time.Time
	previousPublic  int64
	previousSwarm   int64
	traffic         protocol.MirrorTraffic
}

func (c *Counters) BeginPublicDownload() func() {
	if c == nil {
		return func() {}
	}
	c.publicDownloads.Add(1)
	return func() { c.publicDownloads.Add(-1) }
}

func (c *Counters) BeginSwarmUpload() func() {
	if c == nil {
		return func() {}
	}
	c.swarmUploads.Add(1)
	return func() { c.swarmUploads.Add(-1) }
}

func (c *Counters) PublicDownloads() int64 {
	if c == nil {
		return 0
	}
	return c.publicDownloads.Load()
}

func (c *Counters) SwarmUploads() int64 {
	if c == nil {
		return 0
	}
	return c.swarmUploads.Load()
}
