package control

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/node/activity"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestNodeReportsCarryMirrorTrafficSeparatelyFromHostBandwidth(t *testing.T) {
	c := Client{NodeID: "node-1", TargetBandwidthBPS: 10000, Activity: &activity.Counters{}}
	queue := controlv2.NewQueue(8, 1<<20)
	if err := c.enqueueV2Status(queue); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	envelope, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var status protocolv2.NodeStatus
	if err := json.Unmarshal(envelope.Payload, &status); err != nil || status.MirrorTraffic == nil || !status.MirrorTraffic.Valid() {
		t.Fatalf("v2 telemetry missing: %+v err=%v", status, err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	_ = server.SetDeadline(time.Now().Add(3 * time.Second))
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, kind := range []string{protocol.TypeHeartbeat, protocol.TypePressureReport} {
			frame, ok := expectType(t, server, kind)
			if !ok {
				return
			}
			var sample *protocol.MirrorTraffic
			var host int64
			if kind == protocol.TypeHeartbeat {
				var hb protocol.Heartbeat
				_ = json.Unmarshal(frame.Payload, &hb)
				sample, host = hb.Pressure.MirrorTraffic, hb.Pressure.ActualBandwidthBPS
			} else {
				var report protocol.PressureReport
				_ = json.Unmarshal(frame.Payload, &report)
				sample, host = report.MirrorTraffic, report.ActualBandwidthBPS
			}
			if sample == nil || !sample.Valid() || host != 9000 {
				t.Errorf("%s telemetry=%+v host=%d", kind, sample, host)
			}
			sendAck(server, frame)
		}
	}()
	next, err := c.heartbeat(client, "req", 2, 9000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.sendPressureReport(client, "req", next, 9000); err != nil {
		t.Fatal(err)
	}
	<-done
}
