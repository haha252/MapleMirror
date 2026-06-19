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
	written := 0
	chunkSize := w.limiter.writeChunkSize()
	for written < len(p) {
		end := written + chunkSize
		if end > len(p) {
			end = len(p)
		}
		chunk := p[written:end]
		if err := w.limiter.Wait(w.ctx, len(chunk)); err != nil {
			return written, err
		}
		n, err := w.dst.Write(chunk)
		w.limiter.Record(int64(n))
		written += n
		if err != nil {
			return written, err
		}
		if n != len(chunk) {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (l *Limiter) writeChunkSize() int {
	if l == nil || l.TargetBPS <= 0 {
		return maxWriteChunkBytes
	}
	size := l.TargetBPS / 10
	if size > maxWriteChunkBytes {
		size = maxWriteChunkBytes
	}
	if size < minWriteChunkBytes {
		size = minWriteChunkBytes
	}
	return int(size)
}
