package control

import (
	"context"
	"fmt"
	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"testing"
	"time"
)

func TestV2RejectionBarrierWaitsForFreshCapacityAndMinimumBackoff(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		t.Run(fmt.Sprint(versioned), func(t *testing.T) {
			repo, closeDB := testRepo(t)
			defer closeDB()
			session := seedNodeAndSession(t, repo)
			seedV2DispatchTasks(t, repo, session.NodeID, 1)
			revision := uint64(0)
			if versioned {
				revision = 2
			}
			status := protocolv2.NodeStatus{SyncTaskSlotsAvailable: 1, CapacityRevision: revision}
			if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
				t.Fatal(err)
			}
			mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='sent',attempt_id='a1',attempts=5 WHERE id='task-1'`)
			if versioned {
				revision++
			}
			rejected := protocolv2.SyncRejected{TaskID: "task-1", AttemptID: "a1", Reason: "sync task slots full", CapacityRevision: revision}
			if err := repo.rejectV2Task(context.Background(), session, rejected); err != nil {
				t.Fatal(err)
			}
			gate := repo.runtime().v2DispatchState(session.NodeID)
			if time.Until(gate.PauseUntil) < 4*time.Second || !gate.WaitingStatus {
				t.Fatal("missing rejection backoff/barrier")
			}
			status.CapacityRevision = revision
			if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
				t.Fatal(err)
			}
			if got, err := repo.v2DispatchAllowance(context.Background(), session.NodeID); got != 0 || err != nil {
				t.Fatalf("old report opened admission: %d %v", got, err)
			}
			gate.PauseUntil = time.Now().Add(-time.Second)
			repo.runtime().setV2DispatchState(session.NodeID, gate)
			if got, _ := repo.v2DispatchAllowance(context.Background(), session.NodeID); got != 0 {
				t.Fatal("timer alone opened admission")
			}
			if versioned {
				status.CapacityRevision++
			}
			if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
				t.Fatal(err)
			}
			if got, err := repo.v2DispatchAllowance(context.Background(), session.NodeID); got != 1 || err != nil {
				t.Fatalf("fresh report did not recover: %d %v", got, err)
			}
			var attempts int
			if err := repo.DB.QueryRow(`SELECT attempts FROM node_tasks WHERE id='task-1'`).Scan(&attempts); err != nil || attempts != 5 {
				t.Fatalf("history reset: %d %v", attempts, err)
			}
		})
	}
}

func TestV2TaskReplayRefillsOnAcceptanceAndRecoversQueueFailureAndLeaseExpiry(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedV2DispatchTasks(t, repo, session.NodeID, 8)
	if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{SyncTaskSlotsAvailable: 10}); err != nil {
		t.Fatal(err)
	}
	queue := controlv2.NewQueue(1, 1<<20)
	defer queue.Close()
	filler, _ := protocolv2.New(protocolv2.TypeNodeStatus, "filler", protocolv2.NodeStatus{})
	if err := queue.Enqueue(filler, ""); err != nil {
		t.Fatal(err)
	}
	server := &V2Server{Repo: repo}
	if err := server.dispatchV2Tasks(context.Background(), session, queue); err != nil {
		t.Fatal(err)
	}
	var firstAttempt string
	if err := repo.DB.QueryRow(`SELECT attempt_id FROM node_tasks WHERE state='sent'`).Scan(&firstAttempt); err != nil {
		t.Fatal(err)
	}
	if len(queue.ReplayKeys(protocolv2.TypeSyncTask)) != 0 {
		t.Fatal("full queue reserved replay window")
	}
	if _, err := queue.Dequeue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.dispatchV2Tasks(context.Background(), session, queue); err != nil {
		t.Fatal(err)
	}
	env, task := dequeueV2DispatchTask(t, queue)
	if task.AttemptID != firstAttempt {
		t.Fatal("queue failure replaced attempt")
	}
	queue.ReplaySent(env.ID)
	ack, _ := protocolv2.Reply(protocolv2.TypeSyncAccepted, "accept", env.ID, protocolv2.SyncAccepted{TaskID: task.TaskID, AttemptID: task.AttemptID})
	if err := server.handleV2Message(context.Background(), session, queue, ack); err != nil {
		t.Fatal(err)
	}
	if _, tracked := queue.ReplayEnvelope(env.ID); tracked {
		t.Fatal("accepted task retained replay entry")
	}
	_, next := dequeueV2DispatchTask(t, queue)
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET state='cancelled' WHERE id!=? AND state!='running'`, next.TaskID)
	// A lost acceptance cannot hold the pending window beyond its domain lease.
	mustExecControl(t, repo.DB, `UPDATE node_tasks SET lease_expires_at=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), next.TaskID)
	if err := server.dispatchV2Tasks(context.Background(), session, queue); err != nil {
		t.Fatal(err)
	}
	_, renewed := dequeueV2DispatchTask(t, queue)
	if renewed.TaskID != next.TaskID || renewed.AttemptID == next.AttemptID {
		t.Fatalf("expired lease not reclaimed: %+v %+v", next, renewed)
	}
}

func TestV2BatchSyncReachesRoutingReadyAfterCapacityRecovers(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedV2DispatchTasks(t, repo, session.NodeID, 6)
	queue := controlv2.NewQueue(32, 1<<20)
	defer queue.Close()
	server := &V2Server{Repo: repo}
	status := protocolv2.NodeStatus{SyncTaskSlotsAvailable: 2, CapacityRevision: 1}
	if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
		t.Fatal(err)
	}
	for completed := 0; completed < 6; completed += 2 {
		if err := server.dispatchV2Tasks(context.Background(), session, queue); err != nil {
			t.Fatal(err)
		}
		var batch []protocolv2.SyncTask
		for i := 0; i < 2; i++ {
			_, task := dequeueV2DispatchTask(t, queue)
			batch = append(batch, task)
			ok, err := repo.acceptV2Task(context.Background(), session, task.TaskID, task.AttemptID)
			if !ok || err != nil {
				t.Fatalf("accept=%v err=%v", ok, err)
			}
		}
		status.CapacityRevision++
		status.SyncTaskSlotsAvailable = 0
		status.ActiveTasks = []protocolv2.ActiveTask{{TaskID: batch[0].TaskID, AttemptID: batch[0].AttemptID}, {TaskID: batch[1].TaskID, AttemptID: batch[1].AttemptID}}
		if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
			t.Fatal(err)
		}
		for _, task := range batch {
			ok, err := repo.acceptV2SyncResult(context.Background(), session, protocolv2.SyncResult{
				TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: task.Asset.AssetID, Result: "succeeded", SizeBytes: 10,
				LocalDigestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			})
			if !ok || err != nil {
				t.Fatalf("result=%v err=%v", ok, err)
			}
		}
		status.CapacityRevision++
		status.SyncTaskSlotsAvailable = 2
		status.ActiveTasks = nil
		if err := repo.AcceptV2NodeStatus(context.Background(), session, status); err != nil {
			t.Fatal(err)
		}
	}
	var missing, unfinished, ready int
	if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM target_inventory ti LEFT JOIN node_inventory ni
	 ON ni.node_id=ti.node_id AND ni.asset_id=ti.asset_id WHERE ti.node_id=? AND (ni.state IS NULL OR ni.state!='verified')`, session.NodeID).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if err := repo.DB.QueryRow(`SELECT COUNT(*) FROM node_tasks WHERE state IN ('pending','sent','running','retry_wait','failed')`).Scan(&unfinished); err != nil {
		t.Fatal(err)
	}
	if err := repo.DB.QueryRow(`SELECT routing_ready FROM nodes WHERE id=?`, session.NodeID).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if missing != 0 || unfinished != 0 || ready != 1 {
		t.Fatalf("sync did not converge: missing=%d unfinished=%d ready=%d", missing, unfinished, ready)
	}
}

func TestV2ReconnectAndMasterRestartRequireCurrentStatusAndRetainSentIdentity(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedV2DispatchTasks(t, repo, session.NodeID, 1)
	if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{SyncTaskSlotsAvailable: 1}); err != nil {
		t.Fatal(err)
	}
	oldQueue := controlv2.NewQueue(16, 1<<20)
	defer oldQueue.Close()
	server := &V2Server{Repo: repo}
	if err := server.dispatchV2Tasks(context.Background(), session, oldQueue); err != nil {
		t.Fatal(err)
	}
	_, original := dequeueV2DispatchTask(t, oldQueue)
	for _, restart := range []bool{false, true} {
		if restart {
			repo.Runtime = NewRuntimeStore()
			server.Repo = repo
		}
		current, err := repo.StartSession(context.Background(), "sha256:aa", "reconnect")
		if err != nil {
			t.Fatal(err)
		}
		queue := controlv2.NewQueue(16, 1<<20)
		defer queue.Close()
		// Late generic capacity from a replaced v1 session cannot open v2.
		repo.runtime().SetSyncTaskSlotsAvailable(current.NodeID, 10)
		if err := server.dispatchV2Tasks(context.Background(), current, queue); err != nil {
			t.Fatal(err)
		}
		if count, _ := queue.Stats(); count != 0 {
			t.Fatal("reconnect used previous capacity")
		}
		if err := repo.AcceptV2NodeStatus(context.Background(), session, protocolv2.NodeStatus{SyncTaskSlotsAvailable: 10}); err == nil {
			t.Fatal("old session status accepted")
		}
		if err := repo.AcceptV2NodeStatus(context.Background(), current, protocolv2.NodeStatus{SyncTaskSlotsAvailable: 1, CapacityRevision: 1}); err != nil {
			t.Fatal(err)
		}
		if err := server.dispatchV2Tasks(context.Background(), current, queue); err != nil {
			t.Fatal(err)
		}
		_, replayed := dequeueV2DispatchTask(t, queue)
		if replayed.AttemptID != original.AttemptID {
			t.Fatal("reconnect replaced unexpired sent attempt")
		}
		session = current
	}
}
