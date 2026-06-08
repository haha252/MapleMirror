package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

type deadlineAwareExecutor struct{}

func (deadlineAwareExecutor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	<-ctx.Done()
	return protocol.SyncTaskResult{
		TaskID: task.TaskID, AssetID: task.Asset.AssetID,
		Result: "temporary_error", Message: "同步任务执行超时",
	}
}

func TestExecuteTaskAsyncTimesOutAndStoresPendingResult(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{
		NodeID: "node-1", DB: db, Executor: deadlineAwareExecutor{},
		TaskLimiter: NewTaskLimiter(1), TaskTimeout: 20 * time.Millisecond,
	}
	client.executeTaskAsync(protocol.SyncTask{
		TaskID: "task-1", TaskType: "asset_download",
		Asset: protocol.SyncAsset{AssetID: "asset-1"},
	})

	deadline := time.Now().Add(time.Second)
	for {
		var result, state string
		err := db.QueryRow(`SELECT result FROM pending_sync_task_results
			WHERE task_id = 'task-1'`).Scan(&result)
		if err == nil {
			if result != "temporary_error" {
				t.Fatalf("pending result = %q", result)
			}
			if err := db.QueryRow(`SELECT state FROM local_sync_tasks
				WHERE task_id = 'task-1'`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state == "running" {
				t.Fatal("timed out task should not stay running")
			}
			if client.TaskLimiter.Active() != 0 {
				t.Fatalf("worker should be released, active=%d", client.TaskLimiter.Active())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending result was not stored before deadline: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
