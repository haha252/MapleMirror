package control

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestReadOptionalTasksRefillsBeyondTenWhenCapacityAvailable(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	executed := make(chan string, 12)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 12; i++ {
			taskID := fmt.Sprintf("task-%02d", i)
			writeSyncTask(t, server, taskID)
			ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
			if !ok {
				return
			}
			wantSeq := uint64(2 + i)
			if ack.Sequence != wantSeq {
				t.Errorf("%s ack sequence = %d, want %d", taskID, ack.Sequence, wantSeq)
				return
			}
			sendAck(server, ack)
		}
	}()
	ctl := Client{
		NodeID:      "node-1",
		Executor:    recordingExecutor{tasks: executed},
		TaskLimiter: NewTaskLimiter(12),
	}
	next, err := ctl.readOptionalTasksToCapacity(client, "req-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if next != 15 {
		t.Fatalf("next sequence = %d, want 15", next)
	}
	<-done
	seen := map[string]bool{}
	for len(seen) < 12 {
		select {
		case id := <-executed:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("tasks not executed: %+v", seen)
		}
	}
}

func TestReadOptionalTasksAcksBatchInSingleSession(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	executed := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeSyncTask(t, server, "task-1")
		ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok || ack.Sequence != 3 {
			t.Errorf("first ack sequence = %d", ack.Sequence)
			return
		}
		sendAck(server, ack)
		writeSyncTask(t, server, "task-2")
		ack, ok = expectType(t, server, protocol.TypeSyncTaskAck)
		if !ok || ack.Sequence != 4 {
			t.Errorf("second ack sequence = %d", ack.Sequence)
			return
		}
		sendAck(server, ack)
	}()
	ctl := Client{NodeID: "node-1", Executor: recordingExecutor{tasks: executed}}
	next, err := ctl.readOptionalTasks(client, "req-1", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if next != 5 {
		t.Fatalf("next sequence = %d, want 5", next)
	}
	<-done
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case id := <-executed:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("tasks not executed: %+v", seen)
		}
	}
}

func TestReadOptionalTasksReturnsProtocolError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		body, _ := json.Marshal(protocol.ProtocolError{
			Code:    "CONTROL_MESSAGE_ERROR",
			Message: "bad task",
		})
		_ = protocol.WriteFrame(server, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       "err",
			MessageType:     protocol.TypeProtocolError,
			SentAt:          time.Now().UTC(),
			NodeID:          "node-1",
			RequestID:       "req-1",
			Payload:         body,
		})
	}()
	ctl := Client{NodeID: "node-1"}
	if _, err := ctl.readOptionalTasks(client, "req-1", 3, 1); err == nil {
		t.Fatal("expected optional task protocol error to propagate")
	}
	<-done
}

func TestRunOnceReadsTaskDispatchedAfterPressureReport(t *testing.T) {
	executed := make(chan string, 1)
	dialer := newPipeDialer(t, func(conn net.Conn) {
		defer conn.Close()
		hello, ok := expectType(t, conn, protocol.TypeHello)
		if !ok {
			return
		}
		sendWelcome(conn, hello)
		hb, ok := expectType(t, conn, protocol.TypeHeartbeat)
		if !ok {
			return
		}
		sendAck(conn, hb)
		pressure, ok := expectType(t, conn, protocol.TypePressureReport)
		if !ok {
			return
		}
		sendAck(conn, pressure)
		writeSyncTaskFor(t, conn, pressure, "task-after-pressure")
		ack, ok := expectType(t, conn, protocol.TypeSyncTaskAck)
		if !ok {
			return
		}
		if ack.Sequence != 4 {
			t.Errorf("sync task ack sequence = %d, want 4", ack.Sequence)
			return
		}
		sendAck(conn, ack)
	})
	client := &Client{
		NodeID:         "node-1",
		Address:        "master.test:9443",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		DialTLSContext: dialer,
		Executor:       recordingExecutor{tasks: executed},
		TaskLimiter:    NewTaskLimiter(1),
	}
	if _, err := client.RunOnce(); !runOnceEndedByPeer(err) {
		t.Fatal(err)
	}
	select {
	case id := <-executed:
		if id != "task-after-pressure" {
			t.Fatalf("executed task = %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("task dispatched after pressure report was not executed")
	}
}
