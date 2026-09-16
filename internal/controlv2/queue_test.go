package controlv2

import (
	"context"
	"fmt"
	"testing"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func msg(t *testing.T, typ, id string, payload any) protocolv2.Envelope {
	t.Helper()
	e, err := protocolv2.New(typ, id, payload)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestQueuePriority(t *testing.T) {
	q := NewQueue(10, 1<<20)
	_ = q.Enqueue(msg(t, protocolv2.TypeNodeStatus, "low", map[string]int{"x": 1}), "task")
	_ = q.Enqueue(msg(t, protocolv2.TypeSyncResult, "high", map[string]int{"x": 2}), "")
	got, err := q.Dequeue(context.Background())
	if err != nil || got.ID != "high" {
		t.Fatalf("got=%q err=%v", got.ID, err)
	}
}

func TestQueueCoalescesLatest(t *testing.T) {
	q := NewQueue(1, 1<<20)
	if err := q.Enqueue(msg(t, protocolv2.TypeNodeStatus, "1", map[string]int{"x": 1}), "node"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(msg(t, protocolv2.TypeNodeStatus, "2", map[string]int{"x": 2}), "node"); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(context.Background())
	if err != nil || got.ID != "2" {
		t.Fatalf("got=%q err=%v", got.ID, err)
	}
}

func TestQueueFullDoesNotDropDurable(t *testing.T) {
	q := NewQueue(1, 1<<20)
	if err := q.Enqueue(msg(t, protocolv2.TypeTrafficEvent, "1", map[string]int{"x": 1}), ""); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(msg(t, protocolv2.TypeSyncResult, "2", map[string]int{"x": 2}), ""); err != ErrQueueFull {
		t.Fatalf("err=%v", err)
	}
}

func TestQueueBurstFairnessPreventsStarvation(t *testing.T) {
	q := NewQueue(128, 1<<20)
	if err := q.Enqueue(msg(t, protocolv2.TypeNodeStatus, "low", map[string]int{"x": 0}), "low-task"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < defaultHighPriorityBurst+1; i++ {
		id := fmt.Sprintf("high-%d", i)
		if err := q.Enqueue(msg(t, protocolv2.TypeTrafficEvent, id, map[string]int{"x": i}), ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < defaultHighPriorityBurst; i++ {
		got, err := q.Dequeue(context.Background())
		if err != nil || got.ID == "low" {
			t.Fatalf("low priority dequeued before burst cap at %d: id=%q err=%v", i, got.ID, err)
		}
	}
	got, err := q.Dequeue(context.Background())
	if err != nil || got.ID != "low" {
		t.Fatalf("low priority starved after burst cap: id=%q err=%v", got.ID, err)
	}
}
