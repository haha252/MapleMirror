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

func TestRecoverInterruptedLocalTasksOnlyRunsOncePerClient(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{NodeID: "node-1", DB: db}
	if err := client.recoverInterruptedLocalTasksOnce(); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-2', 'asset-2', 'asset_download', 'running', ?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.recoverInterruptedLocalTasksOnce(); err != nil {
		t.Fatal(err)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id = 'task-2'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatalf("same-process reconnect must not interrupt active task, got=%s", state)
	}
	var pending int
	err = db.QueryRow(`SELECT COUNT(*) FROM pending_sync_task_results
		WHERE task_id = 'task-2'`).Scan(&pending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("same-process reconnect queued %d false interruption result(s)", pending)
	}
}
