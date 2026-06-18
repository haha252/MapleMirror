package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestRetryWaitResultSchedulesWake(t *testing.T) {
	original := retryBackoffSchedule
	retryBackoffSchedule = []time.Duration{10 * time.Millisecond}
	defer func() { retryBackoffSchedule = original }()

	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	repo.runtime().StartSession(session)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-1")

	_, err := repo.AcceptSyncTaskResult(context.Background(), session, 1, protocol.SyncTaskResult{
		TaskID: "task-1", AssetID: "asset-1", Result: "temporary_error",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		if repo.runtime().ConsumeSyncTaskWake(session.NodeID) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("retry_wait result did not schedule wake")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
