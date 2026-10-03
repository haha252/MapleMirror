package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/node/eventwake"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2OrphanRunningTaskActuallyRestartsAndWakesReconnectedSession(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks(task_id,asset_id,task_type,state,updated_at,attempt_id)
	 VALUES('task-1','asset-1','asset_download','running','old','attempt-1')`)
	if err != nil {
		t.Fatal(err)
	}
	executor := recordingExecutor{tasks: make(chan string, 1)}
	client := &Client{DB: db, Executor: executor, TaskLimiter: NewTaskLimiter(1), V2Runtime: NewV2Runtime(), EventWake: eventwake.New()}
	queue := controlv2.NewQueue(16, 1<<20)
	defer queue.Close()
	task := protocolv2.SyncTask{TaskID: "task-1", AttemptID: "attempt-1", TaskType: "asset_download", Asset: protocolv2.SyncAsset{AssetID: "asset-1"}}
	envelope, _ := protocolv2.New(protocolv2.TypeSyncTask, protocolv2.StableMessageID(protocolv2.TypeSyncTask, task.TaskID, task.AttemptID), task)
	if err := client.handleV2SyncTask(queue, envelope); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.EventWake.C():
	case <-time.After(time.Second):
		t.Fatal("completion did not wake current session")
	}
	select {
	case <-executor.tasks:
	default:
		t.Fatal("orphan row was falsely accepted without execution")
	}
	if client.TaskLimiter.InFlight() != 0 || len(client.loadV2ActiveTasks()) != 0 {
		t.Fatal("finished task retains lease or slot")
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id='task-1'`).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("state=%s err=%v", state, err)
	}
}

func TestV2StaleResultAndManifestCannotReplaceNewAttemptOutbox(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO local_sync_tasks(task_id,asset_id,task_type,state,updated_at,attempt_id)
	 VALUES('task-1','asset-1','asset_download','running','now','new')`); err != nil {
		t.Fatal(err)
	}
	client := &Client{DB: db}
	newResult := protocolv2.SyncResult{TaskID: "task-1", AttemptID: "new", AssetID: "asset-1", Result: "succeeded"}
	if err := client.storePendingV2Result(newResult); err != nil {
		t.Fatal(err)
	}
	oldResult := newResult
	oldResult.AttemptID = "old"
	oldResult.Result = "temporary_error"
	if err := client.storePendingV2Result(oldResult); err != nil {
		t.Fatal(err)
	}
	if err := client.storePendingV2Manifest(protocolv2.SwarmManifest{ManifestID: "m1", AssetID: "asset-1"}, oldResult); err != nil {
		t.Fatal(err)
	}
	var result, attempt string
	if err := db.QueryRow(`SELECT result,attempt_id FROM pending_sync_task_results WHERE task_id='task-1'`).Scan(&result, &attempt); err != nil || result != "succeeded" || attempt != "new" {
		t.Fatalf("result=%s attempt=%s err=%v", result, attempt, err)
	}
	var manifests int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_swarm_manifests`).Scan(&manifests); err != nil || manifests != 0 {
		t.Fatalf("stale manifest persisted: %d %v", manifests, err)
	}
	ack, _ := protocolv2.Reply(protocolv2.TypeSyncResultAck, "ack-old", protocolv2.StableMessageID(protocolv2.TypeSyncResult, "task-1", "old"), protocolv2.SyncResultAck{TaskID: "task-1", AttemptID: "old"})
	if err := client.handleV2ResultAck(ack); err != nil {
		t.Fatalf("late ack disconnected new session: %v", err)
	}
}

func TestV2ReplacementWaitsUntilOldWorkerReleasesPartial(t *testing.T) {
	runtime := NewV2Runtime()
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.executions["task"] = v2Execution{attemptID: "old", cancel: cancel, done: done}
	client := &Client{V2Runtime: runtime}
	client.cancelV2Execution("task", "old")
	if ctx.Err() == nil {
		t.Fatal("old worker was not cancelled")
	}
	if client.waitV2Execution("task", "old", time.Millisecond) {
		t.Fatal("replacement allowed while old worker still owns partial")
	}
	close(done)
	if !client.waitV2Execution("task", "old", time.Second) {
		t.Fatal("stopped worker prevents replacement")
	}
}

func TestV2TimedOutWorkerDoesNotRenewLease(t *testing.T) {
	runtime := NewV2Runtime()
	ctx, cancel := context.WithCancel(context.Background())
	runtime.executions["task"] = v2Execution{attemptID: "a1", ctx: ctx, cancel: cancel}
	if !runtime.hasExecution("task", "a1") {
		t.Fatal("live worker missing")
	}
	cancel()
	if runtime.hasExecution("task", "a1") {
		t.Fatal("timed-out worker still renews its lease")
	}
}
