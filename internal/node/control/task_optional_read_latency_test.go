package control

import (
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestSendNextPendingTaskResultDoesNotWaitFullOptionalRead(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
		VALUES ('task-1', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', 'now')`)
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := ackOneMessage(t, server, protocol.TypeSyncTaskResult)
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}

	start := time.Now()
	next, sent, err := ctl.sendNextPendingTaskResult(clientConn, "req-1", 7)
	elapsed := time.Since(start)
	if err != nil || !sent || next != 8 {
		t.Fatalf("pending result next=%d sent=%v err=%v", next, sent, err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("pending result optional read waited too long: %s", elapsed)
	}
	<-done
}

func TestSendNextRunningTaskAckDoesNotWaitFullOptionalRead(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	mustExecNode(t, db, `INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, updated_at)
		VALUES ('task-1', 'asset-1', 'asset_download', 'running', 'now')`)
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := ackOneMessage(t, server, protocol.TypeSyncTaskAck)
	ctl := Client{
		NodeID:             "node-1",
		DB:                 db,
		Executor:           recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter:        NewTaskLimiter(1),
		runningTaskAckSent: map[string]time.Time{},
	}

	start := time.Now()
	next, sent, err := ctl.sendNextRunningTaskAck(clientConn, "req-1", 7)
	elapsed := time.Since(start)
	if err != nil || !sent || next != 8 {
		t.Fatalf("running ack next=%d sent=%v err=%v", next, sent, err)
	}
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("running ack optional read waited too long: %s", elapsed)
	}
	<-done
}

func TestRepeatedPendingTaskResultsDoNotAccumulateOptionalReadWait(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	for i := 1; i <= 3; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		mustExecNode(t, db, fmt.Sprintf(`INSERT INTO pending_sync_task_results
			(task_id, asset_id, result, local_digest_sha256, size_bytes, message, created_at)
			VALUES ('%s', 'asset-1', 'succeeded', 'sha256:abc', 12, 'ok', 'now')`, taskID))
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			msg, ok := expectType(t, server, protocol.TypeSyncTaskResult)
			if !ok {
				return
			}
			sendAck(server, msg)
		}
	}()
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 1)},
		TaskLimiter: NewTaskLimiter(1),
	}

	start := time.Now()
	sequence := uint64(7)
	for i := 0; i < 3; i++ {
		next, sent, err := ctl.sendNextPendingTaskResult(clientConn, "req-1", sequence)
		if err != nil || !sent {
			t.Fatalf("pending result %d next=%d sent=%v err=%v", i, next, sent, err)
		}
		sequence = next
	}
	elapsed := time.Since(start)
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("repeated pending results waited too long: %s", elapsed)
	}
	<-done
}

func ackOneMessage(t *testing.T, conn net.Conn, messageType string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		msg, ok := expectType(t, conn, messageType)
		if ok {
			sendAck(conn, msg)
		}
	}()
	return done
}
