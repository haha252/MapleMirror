package syncer

import (
	"io"
	"sync"
	"time"
)

type rateLimitedReader struct {
	reader io.Reader
	shared *BandwidthLimiter
}

// BandwidthLimiter is intentionally shared by every origin/peer sync on one
// node. This gives sync.bandwidth_limit_bps node-global semantics.
type BandwidthLimiter struct {
	limit   int64
	started time.Time
	read    int64
	mu      sync.Mutex
}

func NewBandwidthLimiter(limit int64) *BandwidthLimiter {
	if limit <= 0 {
		return nil
	}
	return &BandwidthLimiter{limit: limit, started: time.Now()}
}

func (e Executor) rateLimitedBody(body io.Reader) io.Reader {
	limiter := e.SyncLimiter
	if limiter == nil && e.BandwidthLimitBPS > 0 {
		limiter = NewBandwidthLimiter(e.BandwidthLimitBPS)
	}
	if limiter == nil {
		return body
	}
	return &rateLimitedReader{reader: body, shared: limiter}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.addAndThrottle(int64(n))
	}
	return n, err
}

func (r *rateLimitedReader) addAndThrottle(n int64) {
	if r.shared == nil || r.shared.limit <= 0 {
		return
	}
	r.shared.mu.Lock()
	defer r.shared.mu.Unlock()
	r.shared.read += n
	want := time.Duration(float64(r.shared.read) / float64(r.shared.limit) * float64(time.Second))
	if sleep := r.shared.started.Add(want).Sub(time.Now()); sleep > 0 {
		time.Sleep(sleep)
	}
}

func (e Executor) sharedRateLimiter() *BandwidthLimiter {
	if e.SyncLimiter != nil {
		return e.SyncLimiter
	}
	return NewBandwidthLimiter(e.BandwidthLimitBPS)
}
