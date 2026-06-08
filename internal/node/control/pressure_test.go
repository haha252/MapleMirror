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
		if heartbeat.Pressure.TargetBandwidthBPS != 12500 {
			t.Errorf("heartbeat target bandwidth = %d", heartbeat.Pressure.TargetBandwidthBPS)
			return
		}
		if heartbeat.Pressure.ActualBandwidthBPS != 600 {
			t.Errorf("heartbeat actual bandwidth = %d", heartbeat.Pressure.ActualBandwidthBPS)
			return
		}
		if heartbeat.Pressure.Ratio != 0.048 {
			t.Errorf("heartbeat pressure ratio = %v", heartbeat.Pressure.Ratio)
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
		if pressure.TargetBandwidthBPS != 12500 || pressure.SampleWindowSeconds <= 0 {
			t.Errorf("unexpected pressure report: %+v", pressure)
			return
		}
		if pressure.ActualBandwidthBPS != 600 || pressure.PressureRatio != 0.048 {
			t.Errorf("pressure bandwidth fields = %+v", pressure)
			return
		}
		if pressure.MaxMirrorProjects != 3 {
			t.Errorf("pressure max mirror projects = %d", pressure.MaxMirrorProjects)
			return
		}
		sendAck(server, report)
	}()
	clientCtl := Client{NodeID: "node-1", TargetBandwidthBPS: 12500, MaxMirrorProjects: 3}
	if err := clientCtl.heartbeat(client, "req-1", 2, 600); err != nil {
		t.Fatal(err)
	}
	if err := clientCtl.sendPressureReport(client, "req-1", 3, 600); err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestNetworkBandwidthSamplerUsesWindowDelta(t *testing.T) {
	values := []uint64{1000, 4600}
	sampler := &NetworkBandwidthSampler{read: func() (uint64, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	}}
	if got := sampler.SampleBandwidthBPS(6 * time.Second); got != 0 {
		t.Fatalf("first sample should establish baseline, got %d", got)
	}
	if got := sampler.SampleBandwidthBPS(6 * time.Second); got != 600 {
		t.Fatalf("sample bandwidth = %d", got)
	}
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
