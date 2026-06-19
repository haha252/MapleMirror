package trafficlimit

import (
	"testing"
	"time"
)

func TestLimiterUsesTargetWhenNoOtherTraffic(t *testing.T) {
	clock := fakeClock{now: time.Unix(0, 0)}
	var total uint64
	limiter := testLimiter(100, 10, &clock, func() (uint64, error) {
		return total, nil
	})
	limiter.refresh(clock.now)
	limiter.Record(100)
	clock.now = clock.now.Add(time.Second)
	total = 100
	if got := limiter.allowedAfterSample(); got != 100 {
		t.Fatalf("allowed = %d, want 100", got)
	}
}

func TestLimiterBacksOffForOtherTraffic(t *testing.T) {
	clock := fakeClock{now: time.Unix(0, 0)}
	var total uint64
	limiter := testLimiter(100, 10, &clock, func() (uint64, error) {
		return total, nil
	})
	limiter.refresh(clock.now)
	limiter.Record(20)
	clock.now = clock.now.Add(time.Second)
	total = 80
	if got := limiter.allowedAfterSample(); got != 40 {
		t.Fatalf("allowed = %d, want 40", got)
	}
}

func TestLimiterKeepsMinimumWhenOtherTrafficExceedsTarget(t *testing.T) {
	clock := fakeClock{now: time.Unix(0, 0)}
	var total uint64
	limiter := testLimiter(100, 10, &clock, func() (uint64, error) {
		return total, nil
	})
	limiter.refresh(clock.now)
	clock.now = clock.now.Add(time.Second)
	total = 150
	if got := limiter.allowedAfterSample(); got != 10 {
		t.Fatalf("allowed = %d, want 10", got)
	}
}

func TestLimiterCapsMinimumAtTarget(t *testing.T) {
	if limiter := New(100, 120, nil); limiter.MinimumBPS != 100 {
		t.Fatalf("minimum = %d, want 100", limiter.MinimumBPS)
	}
}

type fakeClock struct {
	now time.Time
}

func testLimiter(target, minimum int64, clock *fakeClock,
	read func() (uint64, error)) *Limiter {
	return &Limiter{TargetBPS: target, MinimumBPS: minimum,
		SampleEvery: time.Second, Now: func() time.Time { return clock.now },
		HostRead: read}
}

func (l *Limiter) allowedAfterSample() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refresh(l.now())
	return l.allowedBPS
}
