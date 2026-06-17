package control

import (
	"testing"

	"mirror-server/internal/protocol"
)

func TestPendingTaskResultPreservesPeerFallbackAttempted(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	client := Client{NodeID: "node-1", DB: db}
	err := client.storePendingTaskResult(protocol.SyncTaskResult{
		TaskID:                "task-1",
		AssetID:               "asset-1",
		Result:                "temporary_error",
		PeerFallbackAttempted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.loadPendingTaskResults(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].PeerFallbackAttempted {
		t.Fatalf("peer fallback flag lost after round trip: %+v", results)
	}
	var stored int
	if err := db.QueryRow(`SELECT COALESCE(peer_fallback_attempted, 0)
		FROM pending_sync_task_results WHERE task_id = 'task-1'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("stored flag = %d, want 1", stored)
	}
}
