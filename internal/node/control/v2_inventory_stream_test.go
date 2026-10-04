package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2LargeInventoryStreamsBeyondQueueBudgetAndRetriesSameSnapshot(t *testing.T) {
	runtime := NewV2Runtime()
	client := &Client{V2Runtime: runtime}
	// This snapshot is larger than the real 4 MiB outbound queue. No segment
	// requires an independent ACK, including against the historical master.
	for i := 0; i < 8; i++ {
		raw := json.RawMessage(`{"padding":"` + string(bytes.Repeat([]byte("x"), 900<<10)) + `"}`)
		runtime.inventorySegments = append(runtime.inventorySegments, protocolv2.Envelope{
			Type: protocolv2.TypeInventorySnapshotSegment, ID: fmt.Sprintf("segment-%d", i), Payload: raw,
		})
	}
	queue := controlv2.NewQueue(1, 1<<20)
	defer queue.Close()
	var originals []protocolv2.Envelope
	for i := 0; i < 8; i++ {
		runtime.inventoryLastSegmentAt = time.Now().Add(-2 * time.Second)
		if err := client.flushV2Inventory(queue); err != nil {
			t.Fatal(err)
		}
		if count, _ := queue.Stats(); count != 1 {
			t.Fatalf("segment %d: queued=%d", i, count)
		}
		if err := client.flushV2Inventory(queue); err != nil {
			t.Fatal(err)
		}
		if count, _ := queue.Stats(); count != 1 {
			t.Fatal("repeated wake exceeded pacing budget")
		}
		e, err := queue.Dequeue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, e)
	}
	runtime.inventoryLastSegmentAt = time.Now().Add(-2 * time.Second)
	if err := client.flushV2Inventory(queue); err != nil {
		t.Fatal(err)
	}
	if count, _ := queue.Stats(); count != 0 {
		t.Fatal("snapshot retried before ACK timeout")
	}
	runtime.inventorySentAt = time.Now().Add(-controlv2.ReplayRetryAfter - time.Second)
	for i := range originals {
		runtime.inventoryLastSegmentAt = time.Now().Add(-2 * time.Second)
		if err := client.flushV2Inventory(queue); err != nil {
			t.Fatal(err)
		}
		got, err := queue.Dequeue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != originals[i].ID || !bytes.Equal(got.Payload, originals[i].Payload) {
			t.Fatal("lost final ACK changed retry snapshot identity or contents")
		}
	}
}
