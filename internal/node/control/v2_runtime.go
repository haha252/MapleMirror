package control

import (
	"context"
	"sync"
	"time"
)

type v2Execution struct {
	attemptID string
	cancel    context.CancelFunc
}

// V2Runtime owns process-local control.v2 state that must survive WebSocket
// reconnects. It is intentionally referenced by pointer from Client so legacy
// value-receiver methods do not copy synchronization primitives.
type V2Runtime struct {
	mu sync.Mutex

	executions         map[string]v2Execution
	inventoryPending   uint64
	inventoryPendingID string
	inventorySentAt    time.Time
}

func NewV2Runtime() *V2Runtime {
	return &V2Runtime{executions: make(map[string]v2Execution)}
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
	r.mu.Unlock()
}

func (c *Client) v2Runtime() *V2Runtime {
	if c.V2Runtime == nil {
		c.V2Runtime = NewV2Runtime()
	}
	return c.V2Runtime
}
