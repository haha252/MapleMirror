package mirrorsync

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/master/assignment"
	mastercontrol "mirror-server/internal/master/control"
)

func TestAdminCancellationSurvivesReconciliationAndCanBeRetried(t *testing.T) {
	db, store := retryStore(t)
	seedRetryTask(t, db, "task-1", "running", "asset-1")
	runtime := mastercontrol.NewRuntimeStore()
	store.Runtime = runtime
	runtime.StartSession(mastercontrol.Session{ID: "session", NodeID: "node-1"})
	mustExecRetry(t, db, `UPDATE node_tasks SET lease_expires_at=?,attempt_id='a1' WHERE id='task-1'`, time.Now().Add(time.Minute).Format(time.RFC3339Nano))
	ctx := context.Background()
	if err := store.CancelTask(ctx, "node-1", "task-1"); err != nil {
		t.Fatal(err)
	}
	if !runtime.ConsumeSyncTaskWake("node-1") {
		t.Fatal("cancellation did not wake connected worker")
	}
	var lease string
	if err := db.QueryRow(`SELECT COALESCE(lease_expires_at,'') FROM node_tasks WHERE id='task-1'`).Scan(&lease); err != nil || lease != "" {
		t.Fatalf("cancelled lease=%q err=%v", lease, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := assignment.GenerateNodeTasks(ctx, tx, "node-1", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if generated != 0 {
		t.Fatalf("reconciliation revived cancelled task: %d", generated)
	}
	assertRetryTaskState(t, db, "task-1", "cancelled")
	if err := store.RetryTask(ctx, "node-1", "task-1"); err != nil {
		t.Fatal(err)
	}
	assertRetryReset(t, db, "task-1")
}
