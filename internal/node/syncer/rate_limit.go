package syncer

import (
	"io"
	"time"
)

type rateLimitedReader struct {
	reader  io.Reader
	limit   int64
	started time.Time
	read    int64
}

func (e Executor) rateLimitedBody(body io.Reader) io.Reader {
	if e.BandwidthLimitBPS <= 0 {
		return body
	}
	return &rateLimitedReader{
		reader:  body,
		limit:   e.BandwidthLimitBPS,
		started: time.Now(),
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.read += int64(n)
		r.throttle()
	}
	return n, err
}

func (r *rateLimitedReader) throttle() {
	if r.limit <= 0 {
		return
	}
	want := time.Duration(float64(r.read) / float64(r.limit) * float64(time.Second))
	if sleep := r.started.Add(want).Sub(time.Now()); sleep > 0 {
		time.Sleep(sleep)
	}
}
