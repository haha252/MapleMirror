package control

import (
	"context"
	"sync/atomic"
)

type TaskLimiter struct {
	sem    chan struct{}
	active int64
}

func NewTaskLimiter(maxWorkers int) *TaskLimiter {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	return &TaskLimiter{sem: make(chan struct{}, maxWorkers)}
}

func (l *TaskLimiter) Active() int64 {
	if l == nil {
		return 0
	}
	return atomic.LoadInt64(&l.active)
}

func (l *TaskLimiter) Run(ctx context.Context, fn func()) error {
	if l == nil {
		fn()
		return nil
	}
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	atomic.AddInt64(&l.active, 1)
	defer func() {
		atomic.AddInt64(&l.active, -1)
		<-l.sem
	}()
	fn()
	return nil
}
