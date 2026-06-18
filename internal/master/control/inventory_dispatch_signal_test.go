package control

import (
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestCompleteInventoryRepairTasksTriggerDispatch(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().StartSession(session)
	seedAssetTarget(t, repo, session.NodeID)
	mustExecControl(t, repo.DB, `INSERT INTO node_inventory
		(node_id, asset_id, local_digest_sha256, size_bytes, verified_at, state)
		VALUES (?, 'asset-1',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		10, 'old', 'verified')`, session.NodeID)
	msg := envelopeWithPayload(t, session, protocol.TypeInventoryReport, 1, protocol.InventoryReport{
		ReportID: "r-missing", Revision: 1, GeneratedAt: time.Now(), Complete: true,
		Items: []protocol.InventoryItem{},
	})

	result, err := (ControlServer{Repo: repo}).handleMessage(session, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTasksChanged || !result.DispatchSyncTasks {
		t.Fatalf("complete inventory repair should trigger dispatch: %+v", result)
	}
	if !repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
		t.Fatal("complete inventory repair should notify active control session")
	}
}
