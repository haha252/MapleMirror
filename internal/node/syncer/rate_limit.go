package syncer

import (
	"io"
	"sync"
	"time"
)

type rateLimitedReader struct {
	reader io.Reader
	shared *rateLimiter
}

type rateLimiter struct {
	limit   int64
	started time.Time
	read    int64
	mu      sync.Mutex
}

func (e Executor) rateLimitedBody(body io.Reader) io.Reader {
	if e.BandwidthLimitBPS <= 0 {
		return body
	}
	return &rateLimitedReader{
		reader: body,
		shared: &rateLimiter{limit: e.BandwidthLimitBPS, started: time.Now()},
	}
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

func (e Executor) sharedRateLimiter() *rateLimiter {
	if e.BandwidthLimitBPS <= 0 {
		return nil
	}
	return &rateLimiter{limit: e.BandwidthLimitBPS, started: time.Now()}
}
