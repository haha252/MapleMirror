package activity

import (
	"sync"
	"testing"
	"time"
)

func TestTrafficSeparatesPublicAndSwarmAndUsesElapsedWindow(t *testing.T) {
	var c Counters
	now := time.Now()
	c.RecordPublicBytes(999)
	if got := c.sampleTrafficAt(now); got.WindowSeconds != 0 || got.PublicBandwidthBPS != 0 {
		t.Fatalf("first sample must establish baseline: %+v", got)
	}
	c.RecordPublicBytes(12000)
	c.RecordSwarmBytes(6000)
	got := c.sampleTrafficAt(now.Add(3 * time.Second))
	if got.WindowSeconds != 3 || got.PublicBandwidthBPS != 4000 || got.SwarmBandwidthBPS != 2000 {
		t.Fatalf("unexpected rates: %+v", got)
	}
	c.RecordPublicBytes(1000)
	if repeat := c.sampleTrafficAt(now.Add(3500 * time.Millisecond)); *repeat != *got {
		t.Fatalf("nearby reports must reuse the same sample: %+v vs %+v", repeat, got)
	}
	c.RecordPublicBytes(3000)
	got = c.sampleTrafficAt(now.Add(5 * time.Second))
	if got.PublicBandwidthBPS != 2000 || got.SwarmBandwidthBPS != 0 {
		t.Fatalf("coalescing lost traffic or mixed upload types: %+v", got)
	}
	got = c.sampleTrafficAt(now.Add(7 * time.Second))
	if got.WindowSeconds != 2 || got.PublicBandwidthBPS != 0 || got.SwarmBandwidthBPS != 0 {
		t.Fatalf("idle traffic must be a valid zero rate: %+v", got)
	}
}

func TestTrafficCountsConcurrentSuccessfulWritesAndIgnoresInvalidCounts(t *testing.T) {
	var c Counters
	now := time.Now()
	c.sampleTrafficAt(now)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RecordPublicBytes(60)
			c.RecordSwarmBytes(30)
			c.RecordPublicBytes(-1)
			c.RecordSwarmBytes(0)
		}()
	}
	wg.Wait()
	got := c.sampleTrafficAt(now.Add(3 * time.Second))
	if got.PublicBandwidthBPS != 2000 || got.SwarmBandwidthBPS != 1000 {
		t.Fatalf("lost concurrent bytes: %+v", got)
	}
	if got := c.sampleTrafficAt(now.Add(-time.Second)); got.WindowSeconds != 0 {
		t.Fatalf("clock rollback must reset baseline: %+v", got)
	}
	var absent *Counters
	absent.RecordPublicBytes(1)
	absent.RecordSwarmBytes(1)
	if absent.SampleTraffic() != nil {
		t.Fatal("missing counters must remain unknown")
	}
}
