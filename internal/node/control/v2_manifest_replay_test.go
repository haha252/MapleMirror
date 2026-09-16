package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2PendingManifestReplaysAfterClientRebuild(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	manifest := protocolv2.SwarmManifest{ManifestID: "manifest-1", AssetID: "asset-1"}
	result := protocolv2.SyncResult{TaskID: "task-1", AttemptID: "attempt-1", AssetID: "asset-1", Result: "succeeded"}
	first := &Client{DB: db}
	if err := first.storePendingV2Manifest(manifest, result); err != nil {
		t.Fatal(err)
	}

	// Simulate losing the WebSocket/client before any Manifest ACK arrives.
	reconnected := &Client{DB: db}
	queue := controlv2.NewQueue(8, 1<<20)
	defer queue.Close()
	if err := reconnected.enqueuePendingV2Manifests(queue); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	envelope, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Type != protocolv2.TypeSwarmManifestReport ||
		envelope.ID != protocolv2.StableMessageID(protocolv2.TypeSwarmManifestReport, manifest.ManifestID) {
		t.Fatalf("unexpected replay envelope: %+v", envelope)
	}
	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_swarm_manifests WHERE manifest_id='manifest-1'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("manifest outbox was consumed before ACK: %d", pending)
	}
}
