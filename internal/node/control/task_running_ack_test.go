package control

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestShouldLogRunningTaskAckRateLimitsPerTask(t *testing.T) {
	client := Client{
		runningTaskAckLogged: map[string]time.Time{},
	}
	if !client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("first running ack should be logged")
	}
	if client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("duplicate running ack within window should not be logged")
	}
	client.runningTaskAckLogged["task-1"] = time.Now().Add(-runningTaskAckLogInterval - time.Second)
	if !client.shouldLogRunningTaskAck("task-1") {
		t.Fatal("running ack should be logged again after window expires")
	}
	if !client.shouldLogRunningTaskAck("task-2") {
		t.Fatal("different task should have independent log window")
	}
}

func TestSendNextRunningTaskAckRotatesAndRateLimits(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	for idx, taskID := range []string{"task-1", "task-2"} {
		_, err := db.Exec(`INSERT INTO local_sync_tasks
			(task_id, asset_id, task_type, state, updated_at)
			VALUES (?, ?, 'asset_download', 'running', ?)`,
			taskID, "asset-"+taskID, now.Add(time.Duration(idx)*time.Second).Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	seen := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			msg, ok := expectType(t, server, protocol.TypeSyncTaskAck)
			if !ok {
				return
			}
			var ack protocol.SyncTaskAck
			if err := json.Unmarshal(msg.Payload, &ack); err != nil {
				t.Error(err)
				return
			}
			seen <- ack.TaskID
			sendAck(server, msg)
		}
	}()
	ctl := Client{
		NodeID:             "node-1",
		DB:                 db,
		runningTaskAckSent: map[string]time.Time{},
	}
	next, sent, err := ctl.sendNextRunningTaskAck(clientConn, "req-1", 7)
	if err != nil || !sent || next != 8 {
		t.Fatalf("first running ack next=%d sent=%v err=%v", next, sent, err)
	}
	next, sent, err = ctl.sendNextRunningTaskAck(clientConn, "req-1", next)
	if err != nil || !sent || next != 9 {
		t.Fatalf("second running ack next=%d sent=%v err=%v", next, sent, err)
	}
	next, sent, err = ctl.sendNextRunningTaskAck(clientConn, "req-1", next)
	if err != nil || sent || next != 9 {
		t.Fatalf("recent running acks should yield next=%d sent=%v err=%v", next, sent, err)
	}
	<-done
	first := <-seen
	second := <-seen
	if first != "task-1" || second != "task-2" {
		t.Fatalf("running ack order = %s, %s", first, second)
	}
}

func TestSendNextRunningTaskAckSkipsRecentWindowAndReachesLaterTasks(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	for i := 1; i <= 51; i++ {
		taskID := fmt.Sprintf("task-%02d", i)
		_, err := db.Exec(`INSERT INTO local_sync_tasks
			(task_id, asset_id, task_type, state, updated_at)
			VALUES (?, ?, 'asset_download', 'running', ?)`,
			taskID, "asset-"+taskID, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		var ack protocol.SyncTaskAck
		if err := json.Unmarshal(msg.Payload, &ack); err != nil {
			t.Error(err)
			return
		}
		if ack.TaskID != "task-51" {
			t.Fatalf("expected task-51 ack, got %s", ack.TaskID)
		}
		sendAck(server, msg)
	}()
	sentAt := now.Add(-runningTaskAckLogInterval / 2)
	sent := map[string]time.Time{}
	for i := 1; i <= 50; i++ {
		sent[fmt.Sprintf("task-%02d", i)] = sentAt
	}
	ctl := Client{
		NodeID:             "node-1",
		DB:                 db,
		runningTaskAckSent: sent,
	}
	next, sentAck, err := ctl.sendNextRunningTaskAck(clientConn, "req-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !sentAck {
		t.Fatal("expected later running task to be acknowledged")
	}
	if next != 8 {
		t.Fatalf("next sequence = %d, want 8", next)
	}
	<-done
}
