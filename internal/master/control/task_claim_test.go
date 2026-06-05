package control

import (
	"context"
	"testing"
)

func TestNextSyncTaskDoesNotRedispatchClaimedTask(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()

	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")

	task, ok, err := repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || !ok || task.TaskID != "task-1" {
		t.Fatalf("expected first dispatch, ok=%v task=%+v err=%v", ok, task, err)
	}
	task, ok, err = repo.NextSyncTask(context.Background(), session.NodeID)
	if err != nil || ok {
		t.Fatalf("claimed task should not redispatch, ok=%v task=%+v err=%v", ok, task, err)
	}
}
