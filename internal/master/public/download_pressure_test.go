package public

import (
	"context"
	"strconv"
	"testing"
	"time"

	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestAuthorizationAvoidsConstrainedNodeAtAnyRate(t *testing.T) {
	for _, transport := range []string{"legacy", "v2"} {
		for _, mbps := range []int64{1, 10, 100} {
			t.Run(transport+"/"+strconv.FormatInt(mbps, 10)+"Mbps", func(t *testing.T) {
				db := openMaster(t)
				seedRoutableAsset(t, db)
				seedPriorityNode(t, db, "node-2", 50, "2026-01-01T00:00:00Z", "online")
				mustExec(t, db, `UPDATE nodes SET download_priority = 70 WHERE id = 'node-1'`)
				runtime := mastercontrol.NewRuntimeStore()
				repo := mastercontrol.Repository{DB: db, Runtime: runtime}
				session := mastercontrol.Session{ID: "session-node-1", NodeID: "node-1", RequestID: "req-node-1"}
				runtime.StartSession(session)
				rate := mbps * 1000000 / 8
				pressure := &protocol.DownloadPressure{SampledAt: time.Now(), WindowSeconds: 10,
					ObservedPeers: 2, ConstrainedPeers: 2, DeliveryBandwidthBPS: rate, EffectiveBandwidthBPS: rate, LimitedAt: time.Now()}
				if transport == "v2" {
					if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{
						PublicDownloadBaseURL: "https://node-1.example.com",
						TargetBandwidthBPS:    200000000 / 8, ActualBandwidthBPS: rate, PublicActiveDownloads: 2, DownloadPressure: pressure}); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := repo.AcceptPressureReport(context.Background(), session, 1, protocol.PressureReport{
						SampleWindowSeconds: 10, TargetBandwidthBPS: 200000000 / 8, ActualBandwidthBPS: rate, ActiveDownloads: 2, DownloadPressure: pressure}); err != nil {
						t.Fatal(err)
					}
				}
				store := Store{DB: db, Runtime: runtime}
				if got := issuePriorityAuth(t, store).Claims.NodeID; got != "node-2" {
					t.Fatalf("selected constrained %s", got)
				}
				report, err := runtime.LatestPressureReport("node-1")
				if err != nil || report["throughput_limited"] != true || report["pressure_ratio"] != float64(1) {
					t.Fatalf("report=%v err=%v", report, err)
				}
				// Expired evidence must let the higher-priority node receive a trial again.
				// Send a new immutable report rather than mutating stored runtime state.
				expired := *pressure
				expired.LimitedAt = time.Now().Add(-3 * time.Minute)
				pressure = &expired
				if transport == "v2" {
					if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{PublicDownloadBaseURL: "https://node-1.example.com", TargetBandwidthBPS: 200000000 / 8, DownloadPressure: pressure}); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := repo.AcceptPressureReport(context.Background(), session, 2, protocol.PressureReport{SampleWindowSeconds: 10, TargetBandwidthBPS: 200000000 / 8, DownloadPressure: pressure}); err != nil {
						t.Fatal(err)
					}
				}
				if got := issuePriorityAuth(t, store).Claims.NodeID; got != "node-1" {
					t.Fatalf("expired estimate still penalized %s", got)
				}
			})
		}
	}
}
