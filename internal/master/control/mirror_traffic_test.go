package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestMirrorTrafficIsSeparateFromHostRoutingPressure(t *testing.T) {
	for _, transport := range []string{"heartbeat", "pressure", "v2"} {
		for _, reported := range []bool{false, true} {
			t.Run(transport+map[bool]string{true: "/new", false: "/legacy"}[reported], func(t *testing.T) {
				repo, closeDB := testRepo(t)
				defer closeDB()
				session := seedNodeAndSession(t, repo)
				var traffic *protocol.MirrorTraffic
				if reported {
					traffic = &protocol.MirrorTraffic{SampledAt: time.Now(), WindowSeconds: 10,
						PublicBandwidthBPS: 100, SwarmBandwidthBPS: 200}
				}
				ctx := context.Background()
				var err error
				switch transport {
				case "heartbeat":
					_, err = repo.AcceptHeartbeat(ctx, session, 1, protocol.Heartbeat{Pressure: protocol.PressureSample{
						TargetBandwidthBPS: 10000, ActualBandwidthBPS: 9000, Ratio: .9, MirrorTraffic: traffic}})
				case "pressure":
					_, err = repo.AcceptPressureReport(ctx, session, 1, protocol.PressureReport{
						SampleWindowSeconds: 10, TargetBandwidthBPS: 10000, ActualBandwidthBPS: 9000, MirrorTraffic: traffic})
				case "v2":
					err = repo.AcceptV2NodeStatus(ctx, session, protocolv2.NodeStatus{
						TargetBandwidthBPS: 10000, ActualBandwidthBPS: 9000, MirrorTraffic: traffic})
				}
				if err != nil {
					t.Fatal(err)
				}
				read := repo.LatestPressureReport
				if transport == "heartbeat" {
					read = repo.LatestHeartbeat
				}
				report, err := read(ctx, session.NodeID)
				if err != nil || report["actual_bandwidth_bps"] != int64(9000) || report["pressure_ratio"] != float64(.9) {
					t.Fatalf("host pressure changed: %v err=%v", report, err)
				}
				got, _ := report["mirror_traffic"].(*protocol.MirrorTraffic)
				if got != traffic {
					t.Fatalf("mirror sample lost or legacy host bytes used as mirror traffic: %+v", report)
				}
				pressure := repo.runtime().LatestRoutingPressure(session.NodeID, time.Minute)
				if !pressure.Valid || pressure.PressureRatio != .9 {
					t.Fatalf("routing must still account for other host workloads: %+v", pressure)
				}
			})
		}
	}
}

func TestInvalidMirrorTrafficIsRejectedByEveryTransport(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	invalid := &protocol.MirrorTraffic{PublicBandwidthBPS: -1}
	if _, err := repo.AcceptHeartbeat(context.Background(), session, 1, protocol.Heartbeat{
		Pressure: protocol.PressureSample{MirrorTraffic: invalid}}); err == nil {
		t.Fatal("heartbeat accepted negative mirror rate")
	}
	if _, err := repo.AcceptPressureReport(context.Background(), session, 1, protocol.PressureReport{
		SampleWindowSeconds: 10, MirrorTraffic: invalid}); err == nil {
		t.Fatal("pressure report accepted negative mirror rate")
	}
	if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{MirrorTraffic: invalid}); err == nil {
		t.Fatal("v2 accepted negative mirror rate")
	}
}
