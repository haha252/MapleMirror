package control

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/node/capacity"
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
	next, err := clientCtl.heartbeat(client, "req-1", 2, 600)
	if err != nil {
		t.Fatal(err)
	}
	if next != 3 {
		t.Fatalf("heartbeat next sequence=%d, want 3", next)
	}
	next, err = clientCtl.sendPressureReport(client, "req-1", next, 600)
	if err != nil {
		t.Fatal(err)
	}
	if next != 4 {
		t.Fatalf("pressure next sequence=%d, want 4", next)
	}
	<-done
}

func TestNetworkBandwidthSamplerUsesWindowDelta(t *testing.T) {
	now := time.Now()
	values := []uint64{1000, 4600}
	sampler := &NetworkBandwidthSampler{now: func() time.Time { return now }, read: func() (uint64, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	}}
	if got := sampler.SampleBandwidthBPS(6 * time.Second); got != 0 {
		t.Fatalf("first sample should establish baseline, got %d", got)
	}
	now = now.Add(6 * time.Second)
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

func TestNetworkBandwidthSamplerUsesElapsedTimeAndCoalescesEvents(t *testing.T) {
	now := time.Now()
	bytes := uint64(1000)
	reads := 0
	sampler := &NetworkBandwidthSampler{now: func() time.Time { return now }, read: func() (uint64, error) { reads++; return bytes, nil }}
	sampler.SampleBandwidthBPS(30 * time.Second)
	now = now.Add(2 * time.Second)
	bytes += 2000
	if got := sampler.SampleBandwidthBPS(30 * time.Second); got != 1000 {
		t.Fatalf("event-driven sample=%d want 1000", got)
	}
	now = now.Add(100 * time.Millisecond)
	bytes += 100
	if got := sampler.SampleBandwidthBPS(30 * time.Second); got != 1000 || reads != 2 {
		t.Fatalf("coalesced rate=%d reads=%d", got, reads)
	}
	now = now.Add(1900 * time.Millisecond)
	bytes += 1900
	if got := sampler.SampleBandwidthBPS(30 * time.Second); got != 1000 {
		t.Fatalf("lost bytes during coalescing: %d", got)
	}
}

func TestLegacyReportsUseFilesystemCapacity(t *testing.T) {
	dir := t.TempDir()
	manager := capacity.NewManager(dir, dir)
	manager.SafetyBytes = 0
	client := Client{Capacity: manager}
	free := client.legacyFreeBytes()
	if free <= 0 {
		t.Fatalf("legacy free bytes=%d", free)
	}
	release, err := manager.ReserveDownload(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	reserved := client.legacyFreeBytes()
	if reserved >= free {
		t.Fatalf("reservation not reflected free=%d reserved=%d", free, reserved)
	}
}
