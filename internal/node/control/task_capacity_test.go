package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestNoExecutorReportsZeroSyncTaskSlots(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		hb, ok := expectType(t, server, protocol.TypeHeartbeat)
		if !ok {
			return
		}
		var heartbeat protocol.Heartbeat
		if err := json.Unmarshal(hb.Payload, &heartbeat); err != nil {
			t.Error(err)
			return
		}
		if heartbeat.SyncTaskSlotsAvailable == nil || *heartbeat.SyncTaskSlotsAvailable != 0 {
			t.Errorf("heartbeat slots = %+v, want 0", heartbeat.SyncTaskSlotsAvailable)
			return
		}
		sendAck(server, hb)
		report, ok := expectType(t, server, protocol.TypePressureReport)
		if !ok {
			return
		}
		var pressure protocol.PressureReport
		if err := json.Unmarshal(report.Payload, &pressure); err != nil {
			t.Error(err)
			return
		}
		if pressure.SyncTaskSlotsAvailable == nil || *pressure.SyncTaskSlotsAvailable != 0 {
			t.Errorf("pressure slots = %+v, want 0", pressure.SyncTaskSlotsAvailable)
			return
		}
		sendAck(server, report)
	}()
	ctl := Client{NodeID: "node-1"}
	next, err := ctl.heartbeat(client, "req-1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	next, err = ctl.sendPressureReport(client, "req-1", next, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != 4 {
		t.Fatalf("next sequence = %d, want 4", next)
	}
	<-done
}

func TestNoExecutorDoesNotReadOptionalSyncTasks(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ctl := Client{NodeID: "node-1"}
	start := time.Now()
	next, handled, err := ctl.readOptionalTaskWithTimeout(client, "req-1", 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if handled || next != 3 {
		t.Fatalf("no executor should not read tasks, handled=%v next=%d", handled, next)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("no executor should short-circuit before reading, elapsed=%s", elapsed)
	}
}
