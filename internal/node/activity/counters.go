package activity

import "sync/atomic"

type Counters struct {
	publicDownloads atomic.Int64
	swarmUploads    atomic.Int64
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
