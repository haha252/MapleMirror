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
	read        func() (uint64, error)
}

func NewNetworkBandwidthSampler() *NetworkBandwidthSampler {
	return &NetworkBandwidthSampler{read: readNonLoopbackNetworkBytes}
}

func (s *NetworkBandwidthSampler) SampleBandwidthBPS(window time.Duration) int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	read := s.read
	if read == nil {
		read = readNonLoopbackNetworkBytes
	}
	total, err := read()
	if err != nil {
		return 0
	}
	if !s.initialized || total < s.previous {
		s.previous = total
		s.initialized = true
		return 0
	}
	seconds := window.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	delta := total - s.previous
	s.previous = total
	return int64(float64(delta) / seconds)
}
