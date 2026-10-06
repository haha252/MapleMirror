package control

import (
	"context"
	protocolv2 "mirror-server/internal/protocol/v2"
	"sync"
	"time"
)

type v2Execution struct {
	attemptID string
	cancel    context.CancelFunc
	done      <-chan struct{}
	ctx       context.Context
}

// V2Runtime owns process-local control.v2 state that must survive WebSocket
// reconnects. It is intentionally referenced by pointer from Client so legacy
// value-receiver methods do not copy synchronization primitives.
type V2Runtime struct {
	mu sync.Mutex
	// Sampling, admission and completion share this lock so a report cannot
	// combine pre-reservation slots with post-reservation active task identities.
	capacityMu       sync.Mutex
	capacityRevision uint64

	executions             map[string]v2Execution
	inventoryPending       uint64
	inventoryPendingID     string
	inventorySentAt        time.Time
	inventorySegments      []protocolv2.Envelope
	inventoryNextSegment   int
	inventoryLastSegmentAt time.Time
}

func NewV2Runtime() *V2Runtime {
	return &V2Runtime{executions: make(map[string]v2Execution)}
}

func (r *V2Runtime) hasExecution(taskID, attemptID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	execution, ok := r.executions[taskID]
	return ok && execution.attemptID == attemptID && (execution.ctx == nil || execution.ctx.Err() == nil)
}

func (r *V2Runtime) beginSession() {
	if r == nil {
		return
	}
	r.mu.Lock()
	// An ACK from the previous WebSocket can no longer arrive. Keep the durable
	// inventory cursor unchanged and allow the new session to resend the same
	// revision immediately.
	r.inventoryPending = 0
	r.inventoryPendingID = ""
	r.inventorySentAt = time.Time{}
	r.inventorySegments = nil
	r.inventoryNextSegment = 0
	r.inventoryLastSegmentAt = time.Time{}
	r.mu.Unlock()
}

func (c *Client) waitV2Execution(taskID, attemptID string, timeout time.Duration) bool {
	r := c.v2Runtime()
	r.mu.Lock()
	execution, ok := r.executions[taskID]
	r.mu.Unlock()
	if !ok || execution.attemptID != attemptID {
		return true
	}
	if execution.done == nil {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-execution.done:
		return true
	case <-timer.C:
		return false
	}
}

func (c *Client) v2Runtime() *V2Runtime {
	if c.V2Runtime == nil {
		c.V2Runtime = NewV2Runtime()
	}
	return c.V2Runtime
}
