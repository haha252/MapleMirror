package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2ActiveTasksRequireExecutionOrDurableManifest(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, task := range []struct{ id, state string }{
		{"live", "running"}, {"orphan", "running"}, {"stale", "running"},
		{"manifest", "waiting_manifest"}, {"missing-manifest", "waiting_manifest"},
	} {
		if _, err := db.Exec(`INSERT INTO local_sync_tasks
			(task_id,asset_id,task_type,state,updated_at,attempt_id)
			VALUES(?, 'asset-1', 'asset_download', ?, ?, 'attempt-1')`, task.id, task.state, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO pending_swarm_manifests
		(manifest_id,asset_id,task_id,attempt_id,manifest_json,result_json,created_at)
		VALUES('manifest-1','asset-1','manifest','attempt-1','{}','{}',?)`, now); err != nil {
		t.Fatal(err)
	}
	runtime := NewV2Runtime()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.executions["live"] = v2Execution{attemptID: "attempt-1", cancel: cancel}
	runtime.executions["stale"] = v2Execution{attemptID: "attempt-old", cancel: cancel}
	// A rebuilt client must still recognize work belonging to an earlier WSS session.
	client := &Client{DB: db, V2Runtime: runtime}
	tasks := client.loadV2ActiveTasks()
	if len(tasks) != 2 || tasks[0].TaskID != "live" || tasks[1].TaskID != "manifest" {
		t.Fatalf("only live execution and durable manifest should renew leases: %+v", tasks)
	}
}

func TestV2AcceptanceQueueFailurePersistsRetryableResult(t *testing.T) {
	for _, closed := range []bool{false, true} {
		name := "full"
		if closed {
			name = "closed"
		}
		t.Run(name, func(t *testing.T) {
			db := openNodeDB(t)
			defer db.Close()
			queue := controlv2.NewQueue(1, 1<<20)
			defer queue.Close()
			wantErr := controlv2.ErrQueueFull
			if closed {
				queue.Close()
				wantErr = controlv2.ErrQueueClosed
			} else {
				status, _ := protocolv2.New(protocolv2.TypeNodeStatus, "status-1", protocolv2.NodeStatus{})
				if err := queue.Enqueue(status, ""); err != nil {
					t.Fatal(err)
				}
			}
			executor := recordingExecutor{tasks: make(chan string, 1)}
			client := &Client{DB: db, Executor: executor, TaskLimiter: NewTaskLimiter(1), V2Runtime: NewV2Runtime()}
			task := protocolv2.SyncTask{TaskID: "task-1", AttemptID: "attempt-1", TaskType: "asset_download",
				Asset: protocolv2.SyncAsset{AssetID: "asset-1"}}
			envelope, _ := protocolv2.New(protocolv2.TypeSyncTask,
				protocolv2.StableMessageID(protocolv2.TypeSyncTask, task.TaskID, task.AttemptID), task)
			if err := client.handleV2SyncTask(queue, envelope); !errors.Is(err, wantErr) {
				t.Fatalf("acceptance error=%v want %v", err, wantErr)
			}
			var state, result, attempt string
			if err := db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id='task-1'`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "temporary_error" {
				t.Fatalf("unstarted task remains %q", state)
			}
			if err := db.QueryRow(`SELECT result,attempt_id FROM pending_sync_task_results
				WHERE task_id='task-1' AND reported_at IS NULL`).Scan(&result, &attempt); err != nil {
				t.Fatal(err)
			}
			if result != "temporary_error" || attempt != "attempt-1" {
				t.Fatalf("pending failure result=%q attempt=%q", result, attempt)
			}
			if client.TaskLimiter.InFlight() != 0 || len(client.loadV2ActiveTasks()) != 0 {
				t.Fatal("unstarted task still consumes capacity or renews its lease")
			}
			select {
			case <-executor.tasks:
				t.Fatal("executor ran without successful acceptance")
			default:
			}
		})
	}
}
