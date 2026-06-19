package trafficlimit

import (
	"context"
	"io"
	"sync"
	"time"
)

type Limiter struct {
	TargetBPS   int64
	MinimumBPS  int64
	SampleEvery time.Duration
	Now         func() time.Time
	HostRead    func() (uint64, error)

	mu           sync.Mutex
	hostReady    bool
	hostPrevious uint64
	mirrorRead   int64
	lastMirror   int64
	updated      time.Time
	allowance    float64
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
		SampleEvery: time.Second, HostRead: read}
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
	remaining := n
	for remaining > 0 {
		chunk := remaining
		if chunk > 32*1024 {
			chunk = 32 * 1024
		}
		if err := l.waitChunk(ctx, int64(chunk)); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

func (l *Limiter) waitChunk(ctx context.Context, n int64) error {
	for {
		sleep := l.reserve(n)
		if sleep <= 0 {
			return nil
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *Limiter) reserve(n int64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.refresh(now)
	if l.allowedBPS <= 0 {
		l.allowedBPS = l.TargetBPS
	}
	capacity := float64(l.TargetBPS)
	if l.allowance > capacity {
		l.allowance = capacity
	}
	if l.allowance >= float64(n) {
		l.allowance -= float64(n)
		return 0
	}
	need := float64(n) - l.allowance
	wait := time.Duration(need / float64(l.allowedBPS) * float64(time.Second))
	l.allowance = 0
	if wait < time.Millisecond {
		return time.Millisecond
	}
	return wait
}

func (l *Limiter) refresh(now time.Time) {
	if l.updated.IsZero() {
		l.updated = now
		l.allowedBPS = l.TargetBPS
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
	l.allowance += elapsed.Seconds() * float64(l.allowedBPS)
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
		return
	}
	if !l.hostReady || total < l.hostPrevious || window <= 0 {
		l.hostReady, l.hostPrevious = true, total
		l.allowedBPS = l.TargetBPS
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
	l.hostPrevious, l.lastMirror = total, l.mirrorRead
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
