package networkpressure

import (
	"math"
	"net/netip"
	"sync"
	"time"

	"mirror-server/internal/protocol"
)

const sampleInterval = 10 * time.Second

type socketSample struct {
	ID                                      string
	Peer                                    netip.Addr
	Acked, Busy, RwndLimited, SndbufLimited uint64
	Pending                                 uint32
}

type peerActivity struct {
	active int
	until  time.Time
}

// Tracker reads TCP delivery on the node's network namespace, including sockets
// owned by a local reverse proxy. Only recently authorized download peers count.
// The zero value is ready to use; unsupported platforms fail open.
type Tracker struct {
	mu        sync.Mutex
	peerMu    sync.Mutex
	peers     map[netip.Addr]peerActivity
	previous  map[string]socketSample
	last      time.Time
	report    protocol.DownloadPressure
	candidate int64
	recovery  int
	read      func() ([]socketSample, error)
	now       func() time.Time
}

func (t *Tracker) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *Tracker) BeginDownload(address string) func() {
	peer, err := netip.ParseAddr(address)
	if err != nil || peer.IsLoopback() || peer.IsUnspecified() {
		return func() {}
	}
	peer = peer.Unmap()
	t.peerMu.Lock()
	if t.peers == nil {
		t.peers = make(map[netip.Addr]peerActivity)
	}
	p := t.peers[peer]
	p.active++
	t.peers[peer] = p
	t.peerMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.peerMu.Lock()
			defer t.peerMu.Unlock()
			p := t.peers[peer]
			p.active--
			// A local proxy may still be delivering a buffered response after the
			// application's handler has returned.
			p.until = t.clock().Add(protocol.DownloadPressureMaxAge)
			t.peers[peer] = p
		})
	}
}

func (t *Tracker) Sample(target, actual int64) *protocol.DownloadPressure {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	if !t.report.Limited(now) {
		t.report.LimitedAt = time.Time{}
		t.report.EffectiveBandwidthBPS = 0
	}
	if !t.last.IsZero() && now.Sub(t.last) < sampleInterval {
		out := t.report
		return &out
	}
	t.peerMu.Lock()
	peers := make(map[netip.Addr]bool, len(t.peers))
	for peer, p := range t.peers {
		if p.active == 0 && !now.Before(p.until) {
			delete(t.peers, peer)
		} else {
			peers[peer] = true
		}
	}
	t.peerMu.Unlock()
	read := t.read
	if read == nil {
		read = readSockets
	}
	var sockets []socketSample
	var err error
	if len(peers) > 0 {
		sockets, err = read()
	}
	if err != nil {
		// Missing telemetry must not create or extend a bottleneck estimate.
		t.previous = nil
		t.last = now
		t.candidate, t.recovery = 0, 0
		out := t.report
		return &out
	}
	window := now.Sub(t.last)
	previous := t.previous
	t.previous = make(map[string]socketSample)
	observed, constrained := make(map[netip.Addr]bool), make(map[netip.Addr]bool)
	var acked uint64
	for _, s := range sockets {
		if !peers[s.Peer.Unmap()] {
			continue
		}
		t.previous[s.ID] = s
		old, ok := previous[s.ID]
		if !ok || t.last.IsZero() || s.Acked < old.Acked || s.Busy < old.Busy ||
			s.RwndLimited < old.RwndLimited || s.SndbufLimited < old.SndbufLimited {
			continue
		}
		acked += s.Acked - old.Acked
		busy := s.Busy - old.Busy
		if busy == 0 {
			continue
		}
		observed[s.Peer] = true
		// These are conservative engineering thresholds, not BBR's policing
		// detector. Require demand and exclude receiver/local buffer limits.
		if s.Pending > 0 && float64(busy) >= float64(window.Microseconds())*0.5 &&
			float64(s.RwndLimited-old.RwndLimited) <= float64(busy)*0.1 &&
			float64(s.SndbufLimited-old.SndbufLimited) <= float64(busy)*0.1 {
			constrained[s.Peer] = true
		}
	}
	// Keep following a proxy's buffered download while the external TCP socket
	// still shows sustained demand, even if the application's handler ended.
	t.peerMu.Lock()
	for peer := range constrained {
		if p, ok := t.peers[peer]; ok && p.active == 0 {
			p.until = now.Add(protocol.DownloadPressureMaxAge)
			t.peers[peer] = p
		}
	}
	t.peerMu.Unlock()
	t.last = now
	t.report.SampledAt = now.UTC()
	t.report.WindowSeconds = 0
	t.report.DeliveryBandwidthBPS = 0
	if window > 0 && previous != nil {
		t.report.WindowSeconds = window.Seconds()
		t.report.DeliveryBandwidthBPS = int64(float64(acked) / window.Seconds())
	}
	t.report.ObservedPeers, t.report.ConstrainedPeers = len(observed), len(constrained)
	rate := t.report.DeliveryBandwidthBPS
	if target > 0 && rate > 0 && len(constrained) >= 2 && float64(max(actual, rate)) < float64(target)*0.8 {
		if t.candidate > 0 && math.Abs(float64(rate-t.candidate)) <= float64(t.candidate)*0.125 {
			t.report.EffectiveBandwidthBPS = min(target, max(actual, rate))
			t.report.LimitedAt = now.UTC()
		}
		t.candidate, t.recovery = rate, 0
	} else {
		t.candidate = 0
		// Rising delivery proves recovery; low demand does not. Two recovery
		// windows avoid toggling on a single burst.
		if len(observed) >= 2 && t.report.Limited(now) &&
			float64(rate) >= float64(t.report.EffectiveBandwidthBPS)*1.25 {
			t.recovery++
			if t.recovery >= 2 {
				t.report.LimitedAt = time.Time{}
				t.report.EffectiveBandwidthBPS = 0
			}
		} else {
			t.recovery = 0
		}
	}
	out := t.report
	return &out
}
