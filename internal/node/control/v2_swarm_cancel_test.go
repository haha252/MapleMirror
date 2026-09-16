package control

import (
	"testing"
	"time"

	"mirror-server/internal/node/swarmstate"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func TestV2CancelKeepsVerifiedSwarmPiecesShareable(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO local_sync_tasks
		(task_id,asset_id,task_type,state,error_message,updated_at,attempt_id)
		VALUES('task-1','asset-1','asset_download','running',NULL,?,'attempt-1')`, now); err != nil {
		t.Fatal(err)
	}
	registry := swarmstate.New()
	bits := make([]byte, swarm.BitsetBytes(1))
	swarm.Set(bits, 0)
	registry.SetPartial(swarmstate.Partial{AssetID: "asset-1", ManifestID: "manifest-1", Path: "/managed/partial", Bitset: bits, Trusted: append([]byte(nil), bits...)})
	client := &Client{DB: db, Swarm: registry, V2Runtime: NewV2Runtime()}
	body := protocolv2.SyncCancel{TaskID: "task-1", AttemptID: "attempt-1", Reason: "test cancel"}
	envelope, _ := protocolv2.New(protocolv2.TypeSyncCancel, "cancel-1", body)
	if err := client.handleV2Cancel(envelope); err != nil {
		t.Fatal(err)
	}
	partial, ok := registry.Partial("asset-1", "manifest-1")
	if !ok || !swarm.Has(partial.Bitset, 0) || !swarm.Has(partial.Trusted, 0) {
		t.Fatalf("cancel revoked verified piece state: %+v ok=%v", partial, ok)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM local_sync_tasks WHERE task_id='task-1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" {
		t.Fatalf("task state=%q", state)
	}
}
