package control

import (
	"context"
	"testing"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2AvailabilityFullSnapshotMayResetRevision(t *testing.T) {
	runtime := NewRuntimeStore()
	runtime.UpdateSwarmAvailability("node-1", protocolv2.SwarmAvailability{AssetID: "a", ManifestID: "m", Revision: 10, Bitset: []byte{1}})
	runtime.UpdateSwarmAvailability("node-1", protocolv2.SwarmAvailability{AssetID: "a", ManifestID: "m", Revision: 1, Bitset: []byte{2}})
	got := runtime.swarmAvailability("a", "m", "")["node-1"]
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("latest full snapshot not accepted: %v", got)
	}
}

func TestV2CapacityBudgetUsesBothFilesystems(t *testing.T) {
	runtime := NewRuntimeStore()
	runtime.MarkV2Status("node-1", runtimeV2Status{Status: protocolv2.NodeStatus{
		AssetFS:   protocolv2.FilesystemCapacity{Valid: true, AvailableBytes: 2 << 30, ReservedBytes: 128 << 20},
		PartialFS: protocolv2.FilesystemCapacity{Valid: true, AvailableBytes: 1 << 30, ReservedBytes: 128 << 20},
	}, ReportedAt: time.Now()})
	repo := Repository{Runtime: runtime}
	want := int64(1<<30) - int64(128<<20) - v2MasterCapacitySafetyBytes
	if got := repo.v2CapacityBudget("node-1"); got != want {
		t.Fatalf("budget=%d want=%d", got, want)
	}
}

func TestV2UnknownCapacityDoesNotBlockMaster(t *testing.T) {
	repo := Repository{Runtime: NewRuntimeStore()}
	if got := repo.v2CapacityBudget("node-1"); got < 1<<60 {
		t.Fatalf("unknown capacity should defer to node admission, got=%d", got)
	}
}

func TestV2StatusWakeOnlyOnUsefulCapacityOrSlotChange(t *testing.T) {
	base := protocolv2.NodeStatus{
		SyncTaskSlotsAvailable: 1,
		AssetFS:                protocolv2.FilesystemCapacity{Valid: true, AvailableBytes: 1 << 30},
		PartialFS:              protocolv2.FilesystemCapacity{Valid: true, AvailableBytes: 1 << 30},
	}
	if v2StatusShouldWake(base, base, true) {
		t.Fatal("identical periodic node.status must not trigger task reconciliation")
	}
	moreSlots := base
	moreSlots.SyncTaskSlotsAvailable = 2
	if !v2StatusShouldWake(base, moreSlots, true) {
		t.Fatal("new sync slot should wake dispatcher")
	}
	moreSpace := base
	moreSpace.PartialFS.AvailableBytes += 1 << 20
	if !v2StatusShouldWake(base, moreSpace, true) {
		t.Fatal("increased capacity should wake dispatcher")
	}
	noSlots := moreSpace
	noSlots.SyncTaskSlotsAvailable = 0
	if v2StatusShouldWake(base, noSlots, true) {
		t.Fatal("capacity change with no free slots should not wake dispatcher")
	}
}

func TestRuntimeControlProtocolTracksLatestTransport(t *testing.T) {
	runtime := NewRuntimeStore()
	runtime.SetControlProtocol("node-1", "v1")
	if got := runtime.ControlProtocol("node-1"); got != "v1" {
		t.Fatalf("protocol=%q", got)
	}
	runtime.SetControlProtocol("node-1", "v2")
	if got := runtime.ControlProtocol("node-1"); got != "v2" {
		t.Fatalf("protocol=%q", got)
	}
}

func TestV2InventorySegmentReceiptIsIdempotent(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	segment := protocolv2.InventorySnapshotSegment{
		Revision: 1, Segment: 0, Complete: true, GeneratedAt: time.Now().UTC(),
		Items: []protocolv2.InventoryItem{{AssetID: "asset-1", SizeBytes: 10,
			DigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LocalState: "verified"}},
	}
	id := protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, session.NodeID, "1", "0")
	complete, err := repo.AcceptV2InventorySegment(context.Background(), session, segment, id)
	if err != nil || !complete {
		t.Fatalf("first inventory segment complete=%v err=%v", complete, err)
	}
	complete, err = repo.AcceptV2InventorySegment(context.Background(), session, segment, id)
	if err != nil || !complete {
		t.Fatalf("duplicate inventory segment complete=%v err=%v", complete, err)
	}
	var receipts int
	if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM node_inventory_v2_segments WHERE node_id=? AND revision=1`, session.NodeID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt count=%d want=1", receipts)
	}
}

func TestV2InventorySegmentReceiptRejectsConflictingReplay(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	first := protocolv2.InventorySnapshotSegment{Revision: 1, Segment: 0, Complete: false, GeneratedAt: time.Now().UTC()}
	id := protocolv2.StableMessageID(protocolv2.TypeInventorySnapshotSegment, session.NodeID, "1", "0")
	if _, err := repo.AcceptV2InventorySegment(context.Background(), session, first, id); err != nil {
		t.Fatal(err)
	}
	conflict := first
	conflict.Items = []protocolv2.InventoryItem{{AssetID: "different", LocalState: "verified"}}
	if _, err := repo.AcceptV2InventorySegment(context.Background(), session, conflict, id); err == nil {
		t.Fatal("conflicting replay should be rejected")
	}
}

func TestV2StatusPersistenceThrottleIsRuntimeScoped(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	first := protocolv2.NodeStatus{PublicDownloadBaseURL: "https://one.example.test", SyncTaskSlotsAvailable: 1}
	if err := repo.AcceptV2NodeStatus(context.Background(), session, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.PublicDownloadBaseURL = "https://two.example.test"
	if err := repo.AcceptV2NodeStatus(context.Background(), session, second); err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err := repo.DB.QueryRow(`SELECT public_download_base_url FROM nodes WHERE id=?`, session.NodeID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != "https://one.example.test" {
		t.Fatalf("throttled status persisted too early: %q", persisted)
	}
	latest, ok := repo.runtime().LatestV2Status(session.NodeID)
	if !ok || latest.Status.PublicDownloadBaseURL != "https://two.example.test" {
		t.Fatalf("runtime did not keep latest status: %+v ok=%v", latest, ok)
	}

	restarted := repo
	restarted.Runtime = NewRuntimeStore()
	restarted.Runtime.StartSession(session)
	if err := restarted.AcceptV2NodeStatus(context.Background(), session, second); err != nil {
		t.Fatal(err)
	}
	if err := repo.DB.QueryRow(`SELECT public_download_base_url FROM nodes WHERE id=?`, session.NodeID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != "https://two.example.test" {
		t.Fatalf("fresh runtime must not inherit old throttle cursor: %q", persisted)
	}
}
