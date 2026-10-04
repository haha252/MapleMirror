package control

import (
	"context"
	"time"

	"mirror-server/internal/controlv2"
)

func (c Client) controlContext() context.Context {
	if c.controlCtx != nil {
		return c.controlCtx
	}
	return context.Background()
}

func (c *Client) flushV2Inventory(queue *controlv2.Queue) error {
	runtime := c.v2Runtime()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return c.flushV2InventoryLocked(queue, runtime)
}

func (c *Client) flushV2InventoryLocked(queue *controlv2.Queue, runtime *V2Runtime) error {
	if len(runtime.inventorySegments) == 0 {
		return nil
	}
	if time.Since(runtime.inventoryLastSegmentAt) < time.Second {
		return nil
	}
	if runtime.inventoryNextSegment == len(runtime.inventorySegments) {
		if time.Since(runtime.inventorySentAt) < controlv2.ReplayRetryAfter {
			return nil
		}
		// Retry the immutable snapshot under the same revision and message IDs.
		runtime.inventoryNextSegment = 0
	}
	// One segment per one-second replay tick bounds bursts even against an old
	// master that only acknowledges the final segment. Never wait on queue space
	// here: periodic status and Ping must continue while inventory is pending.
	if err := queue.Enqueue(runtime.inventorySegments[runtime.inventoryNextSegment], ""); err != nil {
		if replayBackpressure(err) {
			return nil
		}
		return err
	}
	runtime.inventoryNextSegment++
	runtime.inventoryLastSegmentAt = time.Now()
	if runtime.inventoryNextSegment == len(runtime.inventorySegments) {
		runtime.inventorySentAt = time.Now()
	}
	return nil
}
