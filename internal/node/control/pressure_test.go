package control

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestHeartbeatAndPressureReportUseTargetBandwidth(t *testing.T) {
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
		if heartbeat.Pressure.TargetBandwidthBPS != 12345 {
			t.Errorf("heartbeat target bandwidth = %d", heartbeat.Pressure.TargetBandwidthBPS)
			return
		}
		if heartbeat.MaxMirrorProjects != 3 {
			t.Errorf("heartbeat max mirror projects = %d", heartbeat.MaxMirrorProjects)
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
		if pressure.TargetBandwidthBPS != 12345 || pressure.SampleWindowSeconds <= 0 {
			t.Errorf("unexpected pressure report: %+v", pressure)
			return
		}
		if pressure.MaxMirrorProjects != 3 {
			t.Errorf("pressure max mirror projects = %d", pressure.MaxMirrorProjects)
			return
		}
		sendAck(server, report)
	}()
	clientCtl := Client{NodeID: "node-1", TargetBandwidthBPS: 12345, MaxMirrorProjects: 3}
	if err := clientCtl.heartbeat(client, "req-1", 2); err != nil {
		t.Fatal(err)
	}
	if err := clientCtl.sendPressureReport(client, "req-1", 3); err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestTaskLimiterCapsConcurrentExecution(t *testing.T) {
	limiter := NewTaskLimiter(1)
	started := make(chan struct{}, 2)
	releases := make(chan chan struct{}, 2)
	done := make(chan struct{}, 2)
	run := func(release chan struct{}) {
		_ = limiter.Run(context.Background(), func() {
			started <- struct{}{}
			releases <- release
			<-release
		})
		done <- struct{}{}
	}
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	go run(firstRelease)
	<-started
	go run(secondRelease)
	select {
	case <-started:
		t.Fatal("second task started before first released")
	case <-time.After(50 * time.Millisecond):
	}
	close(<-releases)
	<-done
	<-started
	close(<-releases)
	<-done
}
