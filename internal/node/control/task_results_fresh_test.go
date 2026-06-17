package control

import (
	"testing"
	"time"
)

func TestRunOnceInterruptsFreshLocalRunningTask(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	_, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano))
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
		t.Fatalf("fresh running task should be interrupted, got=%s", state)
	}
	var result string
	err = db.QueryRow(`SELECT result FROM pending_sync_task_results
		WHERE task_id = 'task-1'`).Scan(&result)
	if err != nil || result != "temporary_error" {
		t.Fatalf("interrupted task result=%q err=%v", result, err)
	}
}
