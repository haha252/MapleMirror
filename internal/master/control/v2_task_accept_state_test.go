package control

import (
	"context"
	"testing"
	"time"
)

func TestAcceptV2TaskClearsPreviousTransientError(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
        (id,node_id,asset_id,task_type,state,request_id,created_at,updated_at,
         attempt_id,error_message,retry_after,lease_expires_at)
        VALUES('retry-task',?,'asset-1','asset_download','sent','req',?,?,
               'attempt-2','sync task slots full',?,?)`,
		session.NodeID, now, now, now, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
	ok, err := repo.acceptV2Task(context.Background(), session, "retry-task", "attempt-2")
	if err != nil || !ok {
		t.Fatalf("accept ok=%v err=%v", ok, err)
	}
	var state string
	var errMsg, retry any
	if err := repo.DB.QueryRow(`SELECT state,error_message,retry_after FROM node_tasks WHERE id='retry-task'`).Scan(&state, &errMsg, &retry); err != nil {
		t.Fatal(err)
	}
	if state != "running" || errMsg != nil || retry != nil {
		t.Fatalf("state=%s error=%v retry=%v", state, errMsg, retry)
	}
}
