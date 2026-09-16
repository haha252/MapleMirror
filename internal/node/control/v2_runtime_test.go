package control

import (
	"context"
	"testing"

	"mirror-server/internal/controlv2"
)

func TestV2RuntimeExecutionCancelSurvivesClientRebuild(t *testing.T) {
	runtime := NewV2Runtime()
	ctx, cancel := context.WithCancel(context.Background())
	runtime.mu.Lock()
	runtime.executions["task-1"] = v2Execution{attemptID: "attempt-1", cancel: cancel}
	runtime.mu.Unlock()

	// Simulate the supervisor constructing a fresh Client after WSS reconnect.
	reconnected := &Client{V2Runtime: runtime}
	reconnected.cancelV2Execution("task-1", "attempt-1")
	select {
	case <-ctx.Done():
	default:
		t.Fatal("reconnected client did not cancel the existing execution")
	}
}

func TestV2RuntimeCancelRejectsStaleAttempt(t *testing.T) {
	runtime := NewV2Runtime()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.mu.Lock()
	runtime.executions["task-1"] = v2Execution{attemptID: "attempt-new", cancel: cancel}
	runtime.mu.Unlock()

	client := &Client{V2Runtime: runtime}
	client.cancelV2Execution("task-1", "attempt-old")
	select {
	case <-ctx.Done():
		t.Fatal("stale attempt cancelled the current execution")
	default:
	}
}

func TestV2RuntimeBeginSessionAllowsInventoryResend(t *testing.T) {
	runtime := NewV2Runtime()
	runtime.inventoryPending = 7
	runtime.beginSession()
	if runtime.inventoryPending != 0 {
		t.Fatalf("inventory pending revision=%d", runtime.inventoryPending)
	}
}

func TestV2InventoryRevisionIsReservedBeforeAck(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	client := &Client{DB: db, NodeID: "node-1", V2Runtime: NewV2Runtime()}
	queue := controlv2.NewQueue(16, 1<<20)
	defer queue.Close()
	if err := client.enqueueV2Inventory(queue, true); err != nil {
		t.Fatal(err)
	}
	cursor, err := client.loadInventoryCursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor.NextRevision != 2 || cursor.LastAckedRevision != 0 {
		t.Fatalf("cursor after reserve=%+v", cursor)
	}
	client.v2Runtime().beginSession()
	if err := client.enqueueV2Inventory(queue, true); err != nil {
		t.Fatal(err)
	}
	cursor, err = client.loadInventoryCursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor.NextRevision != 3 || cursor.LastAckedRevision != 0 {
		t.Fatalf("reconnect must reserve a fresh revision: %+v", cursor)
	}
}
