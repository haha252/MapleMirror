package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"mirror-server/internal/protocol"
)

func TestNextSyncTaskDoesNotRedispatchClaimedTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || task.TaskID != "task-1" {
		t.Fatalf("expected first dispatch, ok=%v task=%+v err=%v", ok, task, err)
	}
	task, ok, err = repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || ok {
		t.Fatalf("claimed task should not redispatch, ok=%v task=%+v err=%v", ok, task, err)
	}
}

func TestNextSyncTaskCanContinueBeyondTenClaims(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	for i := 1; i <= 12; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		mustExecControl(t, repo.DB, `INSERT INTO node_tasks
			(id, node_id, task_type, state, request_id, created_at, updated_at)
			VALUES (?, ?, 'inventory_reconcile', 'pending', 'req', ?, ?)`,
			taskID, session.NodeID, taskID, taskID)
	}
	for i := 1; i <= 12; i++ {
		task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
		if err != nil || !ok {
			t.Fatalf("dispatch %d should continue, ok=%v task=%+v err=%v", i, ok, task, err)
		}
		want := fmt.Sprintf("task-%02d", i)
		if task.TaskID != want {
			t.Fatalf("dispatch %d task = %s, want %s", i, task.TaskID, want)
		}
	}
	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || ok {
		t.Fatalf("empty queue should stop dispatching, ok=%v task=%+v err=%v", ok, task, err)
	}
}

func TestWriteSyncTasksDispatchesAvailableWindow(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	for i := 1; i <= 3; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		mustExecControl(t, repo.DB, `INSERT INTO node_tasks
			(id, node_id, task_type, state, request_id, created_at, updated_at)
			VALUES (?, ?, 'inventory_reconcile', 'pending', 'req', ?, ?)`,
			taskID, session.NodeID, taskID, taskID)
	}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 3)
		_, err := (ControlServer{Repo: repo}).writeSyncTasks(serverConn, session, "req-1")
		done <- err
	}()
	for i := 1; i <= 3; i++ {
		msg, err := protocol.ReadFrame(clientConn, protocol.MaxFrameBytes)
		if err != nil {
			t.Fatal(err)
		}
		if msg.MessageType != protocol.TypeSyncTask {
			t.Fatalf("message %d type=%s", i, msg.MessageType)
		}
		var task protocol.SyncTask
		if err := json.Unmarshal(msg.Payload, &task); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("task-%02d", i)
		if task.TaskID != want {
			t.Fatalf("task %d = %s, want %s", i, task.TaskID, want)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
