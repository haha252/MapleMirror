package control

import (
	"context"
	"sync/atomic"
)

type TaskLimiter struct {
	sem    chan struct{}
	active int64
	queued int64
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

func (l *TaskLimiter) Capacity() int {
	if l == nil {
		return maxSyncTasksPerSession
	}
	return cap(l.sem)
}

func (l *TaskLimiter) InFlight() int64 {
	if l == nil {
		return 0
	}
	return atomic.LoadInt64(&l.queued)
}

func (l *TaskLimiter) Available() int {
	if l == nil {
		return maxSyncTasksPerSession
	}
	available := l.Capacity() - int(l.InFlight())
	if available < 0 {
		return 0
	}
	return available
}

func (l *TaskLimiter) Reserve() bool {
	if l == nil {
		return true
	}
	for {
		current := atomic.LoadInt64(&l.queued)
		if current >= int64(l.Capacity()) {
			return false
		}
		if atomic.CompareAndSwapInt64(&l.queued, current, current+1) {
			return true
		}
	}
}

func (l *TaskLimiter) Release() {
	if l != nil {
		atomic.AddInt64(&l.queued, -1)
	}
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
