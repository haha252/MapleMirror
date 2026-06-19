package trafficlimit

import (
	"context"
	"io"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	minWriteChunkBytes = 64 * 1024
	maxWriteChunkBytes = 512 * 1024
	minBurstBytes      = 256 * 1024
	maxBurstBytes      = 2 * 1024 * 1024
)

type Limiter struct {
	TargetBPS   int64
	MinimumBPS  int64
	SampleEvery time.Duration
	Now         func() time.Time
	HostRead    func() (uint64, error)

	mu           sync.Mutex
	bucket       *rate.Limiter
	hostReady    bool
	hostPrevious uint64
	mirrorRead   int64
	lastMirror   int64
	updated      time.Time
	allowedBPS   int64
}

func New(target, minimum int64, read func() (uint64, error)) *Limiter {
	if target <= 0 {
		return nil
	}
	if minimum < 0 {
		minimum = 0
	}
	if minimum > target {
		minimum = target
	}
	return &Limiter{TargetBPS: target, MinimumBPS: minimum,
		SampleEvery: time.Second, HostRead: read,
		bucket: rate.NewLimiter(rate.Limit(target), burstBytes(target))}
}

func (l *Limiter) WrapWriter(ctx context.Context, w io.Writer) io.Writer {
	if l == nil {
		return w
	}
	return writer{ctx: ctx, dst: w, limiter: l}
}

func (l *Limiter) Record(n int64) {
	if l == nil || n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mirrorRead += n
}

func (l *Limiter) Wait(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bucket := l.rateLimiter()
	remaining := n
	for remaining > 0 {
		l.refreshNow()
		chunk := remaining
		if burst := bucket.Burst(); burst > 0 && chunk > burst {
			chunk = burst
		}
		if err := bucket.WaitN(ctx, chunk); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

func (l *Limiter) rateLimiter() *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bucket == nil {
		l.bucket = rate.NewLimiter(rate.Limit(l.TargetBPS), burstBytes(l.TargetBPS))
	}
	return l.bucket
}

func (l *Limiter) refreshNow() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refresh(l.now())
}

func (l *Limiter) refresh(now time.Time) {
	if l.updated.IsZero() {
		l.updated = now
		l.allowedBPS = l.TargetBPS
		l.applyRateLocked(l.allowedBPS)
		l.sampleHostLocked(0)
		return
	}
	elapsed := now.Sub(l.updated)
	if elapsed <= 0 {
		return
	}
	if elapsed >= l.sampleEvery() {
		l.sampleHostLocked(elapsed)
	}
	l.updated = now
}

func (l *Limiter) sampleHostLocked(window time.Duration) {
	read := l.HostRead
	if read == nil {
		return
	}
	total, err := read()
	if err != nil {
		l.allowedBPS = l.TargetBPS
		l.applyRateLocked(l.allowedBPS)
		return
	}
	if !l.hostReady || total < l.hostPrevious || window <= 0 {
		l.hostReady, l.hostPrevious = true, total
		l.allowedBPS = l.TargetBPS
		l.applyRateLocked(l.allowedBPS)
		l.lastMirror = l.mirrorRead
		return
	}
	hostBPS := int64(float64(total-l.hostPrevious) / window.Seconds())
	mirrorBPS := int64(float64(l.mirrorRead-l.lastMirror) / window.Seconds())
	otherBPS := hostBPS - mirrorBPS
	if otherBPS < 0 {
		otherBPS = 0
	}
	allowed := l.TargetBPS - otherBPS
	if allowed < l.MinimumBPS {
		allowed = l.MinimumBPS
	}
	if allowed > l.TargetBPS {
		allowed = l.TargetBPS
	}
	l.allowedBPS = allowed
	l.applyRateLocked(allowed)
	l.hostPrevious, l.lastMirror = total, l.mirrorRead
}

func (l *Limiter) applyRateLocked(bps int64) {
	if bps <= 0 {
		bps = l.TargetBPS
	}
	if l.bucket == nil {
		l.bucket = rate.NewLimiter(rate.Limit(bps), burstBytes(bps))
		return
	}
	l.bucket.SetLimit(rate.Limit(bps))
	l.bucket.SetBurst(burstBytes(bps))
}

func burstBytes(bps int64) int {
	burst := bps / 5
	if burst < minBurstBytes {
		burst = minBurstBytes
	}
	if burst > maxBurstBytes {
		burst = maxBurstBytes
	}
	return int(burst)
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Limiter) sampleEvery() time.Duration {
	if l.SampleEvery > 0 {
		return l.SampleEvery
	}
	return time.Second
}
