package control

import (
	"context"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2PeriodicSweepRecoversExpiredLeaseAndLostRetryTimer(t *testing.T) {
	for _, state := range []string{"running", "retry_wait"} {
		t.Run(state, func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()
			session := seedNodeAndSession(t, repo)
			seedAssetTarget(t, repo, session.NodeID)
			past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
			mustExecControl(t, repo.DB, `INSERT INTO node_tasks
			 (id,node_id,asset_id,task_type,state,attempt_id,lease_expires_at,retry_after,request_id,created_at,updated_at)
			 VALUES('stuck',?,'asset-1','asset_download',?,'old-attempt',?,?,'req',?,?)`, session.NodeID, state, past, past, past, past)
			repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 1)
			queue := controlv2.NewQueue(32, 1<<20)
			defer queue.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			server := &V2Server{Repo: repo}
			done := make(chan struct{})
			go func() { defer close(done); server.v2TaskWakeLoopEvery(ctx, session, queue, 10*time.Millisecond) }()
			envelope, err := queue.Dequeue(ctx)
			if err != nil {
				t.Fatalf("periodic recovery failed: %v", err)
			}
			task, err := protocolv2.Decode[protocolv2.SyncTask](envelope)
			if err != nil || task.TaskID != "stuck" || task.AttemptID == "old-attempt" {
				t.Fatalf("wrong recovered attempt: %+v err=%v", task, err)
			}
			cancel()
			<-done
		})
	}
}

func TestSyncResetFencesOldAttemptCancelsWorkerAndRequeuesMissingAsset(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
	 (id,node_id,asset_id,task_type,state,attempt_id,attempts,lease_expires_at,request_id,created_at,updated_at)
	 VALUES('stuck',?,'asset-1','asset_download','running','old-attempt',9,?,'req',?,?)`, session.NodeID, now, now, now)
	repo.runtime().MarkV2Status(session.NodeID, runtimeV2Status{ReportedAt: time.Now(), Status: protocolv2.NodeStatus{
		ActiveTasks: []protocolv2.ActiveTask{{TaskID: "stuck", AttemptID: "old-attempt"}},
	}})
	if err := repo.SyncReset(context.Background(), session.NodeID, "reset-req", "admin"); err != nil {
		t.Fatal(err)
	}
	var state, attempt string
	var attempts int
	if err := repo.DB.QueryRow(`SELECT state,attempt_id,attempts FROM node_tasks WHERE id='stuck'`).Scan(&state, &attempt, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempt != "" || attempts != 0 {
		t.Fatalf("reset state=%s attempt=%s retries=%d", state, attempt, attempts)
	}
	queue := controlv2.NewQueue(32, 1<<20)
	defer queue.Close()
	server := &V2Server{Repo: repo}
	if err := server.dispatchV2Cancellations(context.Background(), session, queue); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	envelope, err := queue.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, err := protocolv2.Decode[protocolv2.SyncCancel](envelope)
	if err != nil || body.TaskID != "stuck" || body.AttemptID != "old-attempt" {
		t.Fatalf("cancel=%+v err=%v", body, err)
	}
	if ok, err := repo.acceptV2SyncResult(ctx, session, protocolv2.SyncResult{
		TaskID: "stuck", AttemptID: "old-attempt", AssetID: "asset-1", Result: "temporary_error",
	}); !ok || err != nil {
		t.Fatalf("stale completion: ok=%v err=%v", ok, err)
	}
	if err := repo.DB.QueryRow(`SELECT state FROM node_tasks WHERE id='stuck'`).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("stale result undid reset: %s %v", state, err)
	}
}

func TestV2TemporarySwarmOutageKeepsBoundedBackoffAfterFiveFailures(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	repo.runtime().SetControlProtocol(session.NodeID, "v2")
	seedAuthoritativeManifest(t, repo, session.NodeID)
	now := time.Now().UTC()
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
	 (id,node_id,asset_id,task_type,state,attempt_id,attempts,lease_expires_at,request_id,created_at,updated_at)
	 VALUES('stuck',?,'asset-1','asset_download','running','a1',5,?,'req',?,?)`, session.NodeID, now.Add(time.Minute).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if _, err := repo.acceptV2SyncResult(context.Background(), session, protocolv2.SyncResult{TaskID: "stuck", AttemptID: "a1", AssetID: "asset-1", Result: "temporary_error", Message: "peer offline"}); err != nil {
		t.Fatal(err)
	}
	var state, retry string
	if err := repo.DB.QueryRow(`SELECT state,retry_after FROM node_tasks WHERE id='stuck'`).Scan(&state, &retry); err != nil {
		t.Fatal(err)
	}
	when, err := time.Parse(time.RFC3339Nano, retry)
	if state != "retry_wait" || err != nil || when.Sub(now) < 2*time.Minute || when.Sub(now) > 4*time.Minute {
		t.Fatalf("state=%s retry=%s err=%v", state, retry, err)
	}
}

func seedAuthoritativeManifest(t *testing.T, repo Repository, nodeID string) {
	t.Helper()
	mustExecControl(t, repo.DB, `INSERT INTO asset_piece_manifests
	 (id,asset_id,asset_size,asset_sha256,piece_layout_version,piece_size,piece_count,piece_hash_blob,status,created_by_node_id,created_at,updated_at)
	 VALUES ('manifest-1','asset-1',10,
	 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,1048576,1,zeroblob(32),'authoritative',?,'now','now')`, nodeID)
}
