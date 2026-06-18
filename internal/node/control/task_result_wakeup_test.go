package control

import (
	"context"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

type instantExecutor struct {
	done chan struct{}
}

func (e instantExecutor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	close(e.done)
	return protocol.SyncTaskResult{
		TaskID: task.TaskID, AssetID: task.Asset.AssetID, Result: "succeeded",
	}
}

func TestCompletedTaskWakesControlLoopForImmediateResult(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`UPDATE inventory_report_cursor
		SET last_acked_revision = 1, updated_at = ? WHERE id = 1`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	executed := make(chan struct{})
	ctl := Client{
		NodeID:          "node-1",
		DB:              db,
		Executor:        instantExecutor{done: executed},
		TaskLimiter:     NewTaskLimiter(1),
		controlWorkWake: make(chan struct{}, 1),
	}
	resultSeen := make(chan struct{})
	go func() {
		defer close(resultSeen)
		writeSyncTask(t, server, "task-1")
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		sendAck(server, ack)
		<-executed
		result, ok := expectType(t, server, protocol.TypeSyncTaskResult)
		if !ok {
			return
		}
		sendAck(server, result)
	}()
	state := sessionLoopState{
		sequence:      3,
		nextHeartbeat: time.Now().Add(time.Hour),
	}
	next, handled, err := ctl.waitForControlEventOrTask(client, "req-1", state.sequence, controlIdleRead)
	state.sequence = next
	if err != nil || !handled {
		t.Fatalf("initial task read handled=%v err=%v", handled, err)
	}
	next, handled, err = ctl.waitForControlEventOrTask(client, "req-1",
		state.sequence, controlIdleRead)
	state.sequence = next
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("wakeup should not consume a dispatched task")
	}
	sent, err := ctl.sendNextControlWork(client, "req-1", &state)
	if err != nil || !sent {
		t.Fatalf("completed task should wake and send result, sent=%v err=%v", sent, err)
	}
	select {
	case <-resultSeen:
	case <-time.After(time.Second):
		t.Fatal("master did not receive completed task result")
	}
}

func TestCompletedTaskReleasesSlotBeforeStoringPendingResult(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, created_at)
		VALUES ('lock-row', 'asset-lock', 'succeeded', ?)`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	executed := make(chan struct{})
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    instantExecutor{done: executed},
		TaskLimiter: NewTaskLimiter(1),
	}
	if !ctl.reserveSyncTaskSlot() {
		t.Fatal("failed to reserve task slot")
	}
	ctl.executeTaskAsync(protocol.SyncTask{
		TaskID: "task-1", TaskType: "asset_download",
		Asset: protocol.SyncAsset{AssetID: "asset-1"},
	})
	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("task did not execute")
	}
	deadline := time.Now().Add(time.Second)
	for ctl.availableSyncTaskSlots() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := ctl.availableSyncTaskSlots(); got != 1 {
		t.Fatalf("completed task should release receive slot before pending store, got %d", got)
	}
}
