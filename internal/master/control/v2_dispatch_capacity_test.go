package control

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func seedV2DispatchTasks(t *testing.T, repo Repository, nodeID string, count int) {
	t.Helper()
	seedAssetTarget(t, repo, nodeID)
	for i := 1; i <= count; i++ {
		asset := fmt.Sprintf("asset-%d", i)
		if i > 1 {
			mustExecControl(t, repo.DB, `INSERT INTO assets
			 (id,release_id,github_asset_id,file_name,architecture,size_bytes,source_url,digest_sha256,service_state,created_at)
			 SELECT ?,release_id,?,?,architecture,size_bytes,source_url,digest_sha256,service_state,created_at FROM assets WHERE id='asset-1'`, asset, i, asset+".zip")
			mustExecControl(t, repo.DB, `INSERT INTO target_inventory(node_id,asset_id,desired_state,updated_at) VALUES(?,?,'required','now')`, nodeID, asset)
		}
		mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		 (id,node_id,asset_id,task_type,state,request_id,created_at,updated_at)
		 VALUES(?,?,?,'asset_download','pending','req','now','now')`, fmt.Sprintf("task-%d", i), nodeID, asset)
	}
}

func dequeueV2DispatchTask(t *testing.T, queue *controlv2.Queue) (protocolv2.Envelope, protocolv2.SyncTask) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env, err := queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeSyncTask {
		t.Fatalf("task dequeue: type=%s err=%v", env.Type, err)
	}
	task, err := protocolv2.Decode[protocolv2.SyncTask](env)
	if err != nil {
		t.Fatal(err)
	}
	return env, task
}

func TestV2DispatchConcurrentWakeupsRespectSlotsAndReplayWindow(t *testing.T) {
	for _, slots := range []int{0, 1, 3, 10} {
		t.Run(fmt.Sprint(slots), func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()
			session := seedNodeAndSession(t, repo)
			seedV2DispatchTasks(t, repo, session.NodeID, 8)
			if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{SyncTaskSlotsAvailable: slots, CapacityRevision: 1}); err != nil {
				t.Fatal(err)
			}
			queue := controlv2.NewQueue(32, 1<<20)
			defer queue.Close()
			server := &V2Server{Repo: repo}
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := server.dispatchV2Tasks(context.Background(), session, queue); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			want := min(slots, controlv2.ReplayWindow)
			if got, _ := queue.Stats(); got != want {
				t.Fatalf("queued=%d want=%d", got, want)
			}
			seen := map[string]bool{}
			for i := 0; i < want; i++ {
				_, task := dequeueV2DispatchTask(t, queue)
				if seen[task.TaskID] {
					t.Fatalf("duplicate batch task %s", task.TaskID)
				}
				seen[task.TaskID] = true
			}
			var sent int
			if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks WHERE state='sent'`).Scan(&sent); err != nil || sent != want {
				t.Fatalf("claimed=%d want=%d err=%v", sent, want, err)
			}
		})
	}
}

func TestV2AllowanceMatchesExactAttemptRatherThanActivityCount(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedV2DispatchTasks(t, repo, session.NodeID, 3)
	lease := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='succeeded',attempt_id='finished' WHERE id='task-1'`)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='running',attempt_id='new',lease_expires_at=? WHERE id IN ('task-2','task-3')`, lease)
	status := protocolv2.NodeStatus{SyncTaskSlotsAvailable: 2, CapacityRevision: 1, ActiveTasks: []protocolv2.ActiveTask{
		{TaskID: "task-1", AttemptID: "finished"}, {TaskID: "task-2", AttemptID: "old"},
		{TaskID: "task-3", AttemptID: "new"}, {TaskID: "task-3", AttemptID: "new"},
	}}
	if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
		t.Fatal(err)
	}
	got, err := repo.v2DispatchAllowance(context.Background(), session.NodeID)
	if err != nil || got != 1 {
		t.Fatalf("allowance=%d want=1 err=%v", got, err)
	}
	status.CapacityRevision++
	status.ActiveTasks = append([]protocolv2.ActiveTask(nil), status.ActiveTasks...)
	status.ActiveTasks[1].AttemptID = "new"
	previous, _ := repo.runtime().LatestV2Status(session.NodeID)
	if !v2StatusShouldWake(previous.Status, status, true) {
		t.Fatal("matching an in-flight attempt must wake dispatch even when free slot count is unchanged")
	}
	if err := repo.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.v2DispatchAllowance(context.Background(), session.NodeID); got != 0 || err == nil {
		t.Fatalf("database failure allowed claims: %d %v", got, err)
	}
}

func TestV2BusyTaskRetainsRealFailurePolicy(t *testing.T) {
	for _, historical := range []int{0, 5} {
		t.Run(fmt.Sprint(historical), func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()
			session := seedNodeAndSession(t, repo)
			seedV2DispatchTasks(t, repo, session.NodeID, 1)
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='sent',attempt_id='busy',attempts=? WHERE id='task-1'`, historical)
			if err := repo.rejectV2Task(context.Background(), session, protocolv2.SyncRejected{TaskID: "task-1", AttemptID: "busy", Reason: "sync task slots full"}); err != nil {
				t.Fatal(err)
			}
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='running',attempt_id='download',lease_expires_at=? WHERE id='task-1'`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
			ok, err := repo.acceptV2SyncResult(context.Background(), session, protocolv2.SyncResult{TaskID: "task-1", AttemptID: "download", AssetID: "asset-1", Result: "temporary_error", Message: "source failed"})
			if !ok || err != nil {
				t.Fatalf("actual failure: %v %v", ok, err)
			}
			var state, message string
			var attempts int
			if err := repo.DB.QueryRow(`SELECT state,attempts,error_message FROM node_tasks WHERE id='task-1'`).Scan(&state, &attempts, &message); err != nil {
				t.Fatal(err)
			}
			want := "retry_wait"
			if historical == 5 {
				want = "failed"
			}
			if attempts != historical+1 || state != want || message != "source failed" {
				t.Fatalf("failure policy changed: %s %d %s", state, attempts, message)
			}
		})
	}
}

func TestV2BusyRejectionsPreserveFailureBudgetAndFenceDuplicates(t *testing.T) {
	for _, rejected := range []protocolv2.SyncRejected{
		{Code: protocolv2.SyncRejectedSlotsFull, Reason: "sync task slots full"},
		{Code: protocolv2.SyncRejectedPreviousAttemptStopping, Reason: "previous attempt is still stopping"},
		{Reason: "sync task slots full"}, {Reason: "previous attempt is still stopping"},
		{Code: protocolv2.SyncRejectedExecutorUnavailable, Reason: "sync executor unavailable"},
		{Code: "unknown", Reason: "sync task slots full"},
	} {
		t.Run(rejected.Code+rejected.Reason, func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()
			session := seedNodeAndSession(t, repo)
			seedV2DispatchTasks(t, repo, session.NodeID, 1)
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET attempts=3 WHERE id='task-1'`)
			rejected.TaskID = "task-1"
			for i := 0; i < 8; i++ {
				rejected.AttemptID = fmt.Sprint(i)
				mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='sent',attempt_id=?,lease_expires_at=? WHERE id='task-1'`, rejected.AttemptID, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano))
				if err := repo.rejectV2Task(context.Background(), session, rejected); err != nil {
					t.Fatal(err)
				}
				if err := repo.rejectV2Task(context.Background(), session, rejected); err != nil {
					t.Fatal(err)
				}
			}
			want := 3
			if !v2RejectionIsBackpressure(rejected) {
				want += 8
			}
			var state string
			var attempts int
			if err := repo.DB.QueryRow(`SELECT state,attempts FROM node_tasks WHERE id='task-1'`).Scan(&state, &attempts); err != nil || state != "retry_wait" || attempts != want {
				t.Fatalf("state=%s attempts=%d want=%d err=%v", state, attempts, want, err)
			}
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='running',attempt_id='accepted' WHERE id='task-1'`)
			if err := repo.rejectV2Task(context.Background(), session, rejected); err != nil {
				t.Fatal(err)
			}
			rejected.AttemptID = "accepted"
			if err := repo.rejectV2Task(context.Background(), session, rejected); err != nil {
				t.Fatal(err)
			}
			if err := repo.DB.QueryRow(`SELECT state,attempts FROM node_tasks WHERE id='task-1'`).Scan(&state, &attempts); err != nil || state != "running" || attempts != want {
				t.Fatalf("late rejection changed accepted task: %s %d %v", state, attempts, err)
			}
		})
	}
}
