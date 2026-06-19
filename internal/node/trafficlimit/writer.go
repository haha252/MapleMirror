package trafficlimit

import (
	"context"
	"io"
)

type writer struct {
	ctx     context.Context
	dst     io.Writer
	limiter *Limiter
}

func (w writer) Write(p []byte) (int, error) {
	if w.ctx == nil {
		w.ctx = context.Background()
	}
	if err := w.limiter.Wait(w.ctx, len(p)); err != nil {
		return 0, err
	}
	n, err := w.dst.Write(p)
	w.limiter.Record(int64(n))
	return n, err
}
