package protocol

import (
	"math"
	"testing"
	"time"
)

func TestDownloadPressureRoutingRatio(t *testing.T) {
	now := time.Now()
	fresh := &DownloadPressure{EffectiveBandwidthBPS: 1, LimitedAt: now}
	stale := &DownloadPressure{EffectiveBandwidthBPS: 1, LimitedAt: now.Add(-3 * time.Minute)}
	future := &DownloadPressure{EffectiveBandwidthBPS: 1, LimitedAt: now.Add(time.Hour)}
	for _, tc := range []struct {
		name           string
		p              *DownloadPressure
		actual, target int64
		want           float64
	}{
		{"legacy node", nil, 1, 200, 0.005},
		{"constrained", fresh, 1, 200, 1},
		{"cooldown while idle", fresh, 0, 200, 0.9},
		{"stale", stale, 1, 200, 0.005},
		{"future", future, 1, 200, 0.005},
		{"unknown target", fresh, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.RoutingRatio(tc.actual, tc.target, now); got != tc.want {
				t.Fatalf("ratio=%v want %v", got, tc.want)
			}
		})
	}
}

func TestDownloadPressureRejectsInvalidEvidence(t *testing.T) {
	for _, p := range []*DownloadPressure{
		{WindowSeconds: -1}, {WindowSeconds: math.NaN()}, {WindowSeconds: math.Inf(1)},
		{ObservedPeers: -1}, {ConstrainedPeers: -1}, {ObservedPeers: 1, ConstrainedPeers: 2},
		{DeliveryBandwidthBPS: -1}, {EffectiveBandwidthBPS: -1, LimitedAt: time.Now()},
	} {
		if p.Valid() || p.Limited(time.Now()) {
			t.Fatalf("accepted invalid evidence: %+v", p)
		}
	}
}
