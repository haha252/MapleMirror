package networkpressure

import (
	"errors"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func TestTrackerDynamicCapacityAndRecovery(t *testing.T) {
	for _, mbps := range []int64{1, 10, 100} {
		t.Run(strconv.FormatInt(mbps, 10)+"Mbps", func(t *testing.T) {
			tracker, step, target := fixture(t, 2)
			rate := mbps * 1000000 / 8
			step(rate, 1, 0, 0, 0)
			if got := tracker.Sample(target, rate); !got.LimitedAt.IsZero() {
				t.Fatal("single window classified as limited")
			}
			step(rate, 1, 0, 0, 0)
			got := tracker.Sample(target, rate)
			if got.EffectiveBandwidthBPS != rate || got.LimitedAt.IsZero() {
				t.Fatalf("capacity=%+v", got)
			}
			// A subsequent lower plateau changes the estimate, without a fixed Mbps cutoff.
			step(rate/2, 1, 0, 0, 0)
			step(rate/2, 1, 0, 0, 0)
			if got := tracker.Sample(target, rate/2); got.EffectiveBandwidthBPS != rate/2 {
				t.Fatalf("lower plateau=%+v", got)
			}
			// No backlog and increasing delivery for two windows prove recovery.
			step(target, 0, 0, 0, 0)
			step(target, 0, 0, 0, 0)
			if got := tracker.Sample(target, target); !got.LimitedAt.IsZero() {
				t.Fatalf("did not recover: %+v", got)
			}
		})
	}
}

func TestTrackerDoesNotMistakeLowDemandOrSlowClientsForCapacity(t *testing.T) {
	for _, tc := range []struct {
		name               string
		peers              int
		pending            uint32
		rwnd, sndbuf, idle uint64
	}{
		{"single client", 1, 1, 0, 0, 0},
		{"no queued demand", 2, 0, 0, 0, 0},
		{"receiver window limited", 2, 1, 8000000, 0, 0},
		{"local send buffer limited", 2, 1, 0, 8000000, 0},
		{"mostly idle", 2, 1, 0, 0, 8000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker, step, target := fixture(t, tc.peers)
			for range 3 {
				step(125000, tc.pending, tc.rwnd, tc.sndbuf, tc.idle)
			}
			if got := tracker.Sample(target, 125000); !got.LimitedAt.IsZero() {
				t.Fatalf("false positive: %+v", got)
			}
		})
	}
}

func TestTrackerRejectsTransientPlateauAndKeepsCooldown(t *testing.T) {
	tracker, step, target := fixture(t, 2)
	step(125000, 1, 0, 0, 0)
	step(1250000, 1, 0, 0, 0)
	if got := tracker.Sample(target, 1250000); !got.LimitedAt.IsZero() {
		t.Fatalf("unstable rate classified: %+v", got)
	}
	step(1250000, 1, 0, 0, 0)
	limitedAt := tracker.report.LimitedAt
	if limitedAt.IsZero() {
		t.Fatal("stable rate was not classified")
	}
	tracker.read = func() ([]socketSample, error) { return nil, errors.New("telemetry unavailable") }
	step(0, 0, 0, 0, 0)
	if !tracker.report.LimitedAt.Equal(limitedAt) {
		t.Fatal("failed sample refreshed evidence")
	}
	tracker.now = func() time.Time { return limitedAt.Add(3 * time.Minute) }
	if got := tracker.Sample(target, 0); !got.LimitedAt.IsZero() {
		t.Fatalf("cooldown never expired: %+v", got)
	}
}

func TestTrackerCountsDistinctPeersNotRangeConnections(t *testing.T) {
	tracker, step, target := fixture(t, 2)
	read := tracker.read
	tracker.read = func() ([]socketSample, error) {
		items, err := read()
		for i := range items {
			items[i].Peer = netip.MustParseAddr("192.0.2.1")
		}
		return items, err
	}
	for range 3 {
		step(125000, 1, 0, 0, 0)
	}
	if !tracker.report.LimitedAt.IsZero() {
		t.Fatal("two connections to same peer classified as node bottleneck")
	}
	_ = tracker.Sample(target, 0)
}

func TestTrackerDoesNotBlockDownloadsDuringKernelRead(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tracker := &Tracker{read: func() ([]socketSample, error) {
		close(entered)
		<-release
		return nil, nil
	}}
	defer tracker.BeginDownload("192.0.2.1")()
	go func() { tracker.Sample(25000000, 0); close(finished) }()
	<-entered
	registered := make(chan struct{})
	go func() { tracker.BeginDownload("192.0.2.2")(); close(registered) }()
	select {
	case <-registered:
	case <-time.After(time.Second):
		close(release)
		<-finished
		t.Fatal("kernel sampling blocked a new download")
	}
	close(release)
	<-finished
}

func TestTrackerFollowsBufferedProxyUntilDeliveryStops(t *testing.T) {
	now := time.Now()
	sockets := []socketSample{
		{ID: "a", Peer: netip.MustParseAddr("192.0.2.1")},
		{ID: "b", Peer: netip.MustParseAddr("192.0.2.2")},
	}
	tracker := &Tracker{now: func() time.Time { return now }, read: func() ([]socketSample, error) { return sockets, nil }}
	for _, s := range sockets {
		// Application has finished; a proxy is still sending the response.
		tracker.BeginDownload(s.Peer.String())()
	}
	tracker.Sample(25000000, 0)
	for range 20 {
		now = now.Add(sampleInterval)
		for i := range sockets {
			sockets[i].Acked += 625000
			sockets[i].Busy += 10000000
			sockets[i].Pending = 1
		}
		tracker.Sample(25000000, 125000)
	}
	if got := tracker.Sample(25000000, 125000); got.ConstrainedPeers != 2 || !got.Limited(now) {
		t.Fatalf("lost buffered proxy demand after two minutes: %+v", got)
	}
	sockets = nil
	now = now.Add(3 * time.Minute)
	if got := tracker.Sample(25000000, 0); got.Limited(now) || len(tracker.peers) != 0 {
		t.Fatalf("idle peers/evidence never expired: %+v", got)
	}
}

// Rates are the aggregate ACKed goodput across the registered peers.
func fixture(t *testing.T, peers int) (*Tracker, func(int64, uint32, uint64, uint64, uint64), int64) {
	t.Helper()
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	sockets := make([]socketSample, peers)
	tracker := &Tracker{now: func() time.Time { return now }, read: func() ([]socketSample, error) { return append([]socketSample(nil), sockets...), nil }}
	for i := range sockets {
		peer := netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)})
		sockets[i] = socketSample{ID: peer.String(), Peer: peer}
		t.Cleanup(tracker.BeginDownload(peer.String()))
	}
	target := int64(200000000 / 8)
	tracker.Sample(target, 0)
	step := func(rate int64, pending uint32, rwnd, sndbuf, idle uint64) {
		now = now.Add(sampleInterval)
		for i := range sockets {
			sockets[i].Acked += uint64(rate * 10 / int64(peers))
			sockets[i].Busy += 10000000 - idle
			sockets[i].RwndLimited += rwnd
			sockets[i].SndbufLimited += sndbuf
			sockets[i].Pending = pending
		}
		tracker.Sample(target, rate)
	}
	return tracker, step, target
}
