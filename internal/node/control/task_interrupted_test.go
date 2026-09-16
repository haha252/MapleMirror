package control

import (
	"testing"
	"time"
)

func TestRunOnceClearsInterruptedLocalRunningTasks(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	client := Client{NodeID: "node-1", DB: db}
	if err := client.resetInterruptedLocalTasks(); err != nil {
		t.Fatal(err)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id = 'task-1'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	if state != "interrupted" {
		t.Fatalf("控制连接重新建立前应清理本地 running 状态，got=%s", state)
	}
	var result string
	err = db.QueryRow(`SELECT result FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&result)
	if err != nil || result != "temporary_error" {
		t.Fatalf("interrupted task should enqueue temporary_error, result=%q err=%v", result, err)
	}
}

func TestRecoverInterruptedLocalTasksPreservesAttemptID(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at, attempt_id)
		VALUES ('task-v2', 'asset-v2', 'asset_download', 'running', ?, 'attempt-7')`, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedLocalTasks(db); err != nil {
		t.Fatal(err)
	}
	var result, attempt string
	err = db.QueryRow(`SELECT result, COALESCE(attempt_id,'') FROM pending_sync_task_results
		WHERE task_id = 'task-v2'`).Scan(&result, &attempt)
	if err != nil || result != "temporary_error" || attempt != "attempt-7" {
		t.Fatalf("startup recovery result=%q attempt=%q err=%v", result, attempt, err)
	}
}
