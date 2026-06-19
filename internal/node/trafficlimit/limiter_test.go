package trafficlimit

import (
	"bytes"
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

func TestLimiterConfiguresTokenBucketBurst(t *testing.T) {
	limiter := New(10*1024*1024, 0, nil)
	if got := limiter.rateLimiter().Burst(); got != maxBurstBytes {
		t.Fatalf("burst = %d, want %d", got, maxBurstBytes)
	}
}

func TestLimiterUpdatesTokenBucketRateAfterSampling(t *testing.T) {
	clock := fakeClock{now: time.Unix(0, 0)}
	var total uint64
	limiter := testLimiter(100, 10, &clock, func() (uint64, error) {
		return total, nil
	})
	limiter.refresh(clock.now)
	limiter.Record(20)
	clock.now = clock.now.Add(time.Second)
	total = 80
	limiter.refresh(clock.now)
	if got := int64(limiter.rateLimiter().Limit()); got != 40 {
		t.Fatalf("bucket limit = %d, want 40", got)
	}
}

func TestWriterFlushesBoundedChunks(t *testing.T) {
	var dst chunkRecordingWriter
	limiter := New(10*1024*1024, 0, nil)
	data := bytes.Repeat([]byte("a"), maxWriteChunkBytes*2+17)
	n, err := limiter.WrapWriter(nil, &dst).Write(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("written = %d, want %d", n, len(data))
	}
	if len(dst.chunks) != 3 {
		t.Fatalf("write chunks = %v, want 3 chunks", dst.chunks)
	}
	for i, size := range dst.chunks {
		if size > maxWriteChunkBytes {
			t.Fatalf("chunk %d size = %d, want <= %d", i, size, maxWriteChunkBytes)
		}
	}
}

func TestWriteChunkSizeKeepsFloorForSlowLimits(t *testing.T) {
	limiter := New(64*1024, 0, nil)
	if got := limiter.writeChunkSize(); got != minWriteChunkBytes {
		t.Fatalf("chunk size = %d, want %d", got, minWriteChunkBytes)
	}
}

func TestWriteChunkSizeCapsFastLimits(t *testing.T) {
	limiter := New(100*1024*1024, 0, nil)
	if got := limiter.writeChunkSize(); got != maxWriteChunkBytes {
		t.Fatalf("chunk size = %d, want %d", got, maxWriteChunkBytes)
	}
}

type fakeClock struct {
	now time.Time
}

type chunkRecordingWriter struct {
	chunks []int
}

func (w *chunkRecordingWriter) Write(p []byte) (int, error) {
	w.chunks = append(w.chunks, len(p))
	return len(p), nil
}

func testLimiter(target, minimum int64, clock *fakeClock,
	read func() (uint64, error)) *Limiter {
	limiter := New(target, minimum, read)
	limiter.SampleEvery = time.Second
	limiter.Now = func() time.Time { return clock.now }
	return limiter
}

func (l *Limiter) allowedAfterSample() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refresh(l.now())
	return l.allowedBPS
}
