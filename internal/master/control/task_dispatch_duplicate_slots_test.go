package control

import (
	"testing"

	"mirror-server/internal/protocol"
)

func TestDuplicateMessageRefreshesSlotsWithoutDispatching(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	if err := repo.runtime().UpdateSequence(session, 10); err != nil {
		t.Fatal(err)
	}
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 0)

	slots := 5
	control := ControlServer{Repo: repo}
	result, err := control.handleMessage(session, envelopeWithPayload(t, session, protocol.TypeHeartbeat, 9, protocol.Heartbeat{
		Status: "syncing", SyncTaskSlotsAvailable: &slots,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.DispatchSyncTasks {
		t.Fatal("duplicate message should not dispatch")
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 5 {
		t.Fatalf("duplicate message should refresh slots: %+v", result)
	}
	if got, known := repo.runtime().SyncTaskDispatchCapacity(session.NodeID); got != 5 || !known {
		t.Fatalf("duplicate message should refresh runtime slots, got=%d known=%v", got, known)
	}
}
