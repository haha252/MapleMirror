package control

import (
	"sync"
	"time"
)

type BandwidthSampler interface {
	SampleBandwidthBPS(window time.Duration) int64
}

type NetworkBandwidthSampler struct {
	mu          sync.Mutex
	initialized bool
	previous    uint64
	sampledAt   time.Time
	lastBPS     int64
	now         func() time.Time
	read        func() (uint64, error)
}

func NewNetworkBandwidthSampler() *NetworkBandwidthSampler {
	return &NetworkBandwidthSampler{read: readNonLoopbackNetworkBytes}
}

func ReadNonLoopbackNetworkBytes() (uint64, error) {
	return readNonLoopbackNetworkBytes()
}

func (s *NetworkBandwidthSampler) SampleBandwidthBPS(_ time.Duration) int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if s.initialized && now.Sub(s.sampledAt) < time.Second && !now.Before(s.sampledAt) {
		return s.lastBPS
	}
	read := s.read
	if read == nil {
		read = readNonLoopbackNetworkBytes
	}
	total, err := read()
	if err != nil {
		return 0
	}
	if !s.initialized || total < s.previous || !now.After(s.sampledAt) {
		s.previous = total
		s.initialized = true
		s.sampledAt = now
		s.lastBPS = 0
		return 0
	}
	seconds := now.Sub(s.sampledAt).Seconds()
	delta := total - s.previous
	s.previous = total
	s.sampledAt = now
	s.lastBPS = int64(float64(delta) / seconds)
	return s.lastBPS
}
