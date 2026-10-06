package control

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

type capacityFeedbackExecutor struct {
	started chan protocol.SyncTask
	finish  chan struct{}
}

func (e capacityFeedbackExecutor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	e.started <- task
	select {
	case <-e.finish:
	case <-ctx.Done():
	}
	return protocol.SyncTaskResult{TaskID: task.TaskID, AssetID: task.Asset.AssetID, Result: "succeeded"}
}

func capacityFeedbackTask(id string) protocolv2.Envelope {
	task := protocolv2.SyncTask{TaskID: id, AttemptID: "attempt-1", TaskType: "asset_download", Asset: protocolv2.SyncAsset{AssetID: "asset-1"}}
	env, _ := protocolv2.New(protocolv2.TypeSyncTask, protocolv2.StableMessageID(protocolv2.TypeSyncTask, id, task.AttemptID), task)
	return env
}

func TestV2CapacityRejectIncludesCodeBarrierAndFreshReport(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	client := &Client{NodeID: "node-1", DB: db, V2Runtime: NewV2Runtime(), TaskLimiter: NewTaskLimiter(1), Executor: recordingExecutor{tasks: make(chan string, 1)}}
	if !client.TaskLimiter.Reserve() {
		t.Fatal("reserve")
	}
	defer client.TaskLimiter.Release()
	queue := controlv2.NewQueue(16, 1<<20)
	defer queue.Close()
	if err := client.enqueueV2Status(queue); err != nil {
		t.Fatal(err)
	}
	if err := client.handleV2SyncTask(queue, capacityFeedbackTask("blocked")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env, err := queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeSyncRejected {
		t.Fatalf("reject=%s err=%v", env.Type, err)
	}
	reject, err := protocolv2.Decode[protocolv2.SyncRejected](env)
	if err != nil || reject.Code != protocolv2.SyncRejectedSlotsFull || reject.CapacityRevision == 0 {
		t.Fatalf("reject=%+v err=%v", reject, err)
	}
	env, err = queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeNodeStatus {
		t.Fatalf("status=%s err=%v", env.Type, err)
	}
	status, err := protocolv2.Decode[protocolv2.NodeStatus](env)
	if err != nil || status.CapacityRevision <= reject.CapacityRevision || status.SyncTaskSlotsAvailable != 0 {
		t.Fatalf("status=%+v reject=%+v err=%v", status, reject, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_sync_tasks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected task recorded as executing: %d %v", count, err)
	}
}

func TestV2ExecutionFeedbackReleasesSlotBeforeSendingDurableResult(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	executor := capacityFeedbackExecutor{started: make(chan protocol.SyncTask, 1), finish: make(chan struct{})}
	client := &Client{NodeID: "node-1", DB: db, V2Runtime: NewV2Runtime(), TaskLimiter: NewTaskLimiter(1), Executor: executor}
	queue := controlv2.NewQueue(16, 1<<20)
	defer queue.Close()
	var release sync.Once
	defer func() {
		release.Do(func() { close(executor.finish) })
		client.waitV2Execution("task-1", "attempt-1", time.Second)
	}()
	if err := client.handleV2SyncTask(queue, capacityFeedbackTask("task-1")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env, err := queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeSyncAccepted {
		t.Fatalf("accept=%s err=%v", env.Type, err)
	}
	env, err = queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeNodeStatus {
		t.Fatalf("accept capacity=%s err=%v", env.Type, err)
	}
	acceptedStatus, _ := protocolv2.Decode[protocolv2.NodeStatus](env)
	if acceptedStatus.SyncTaskSlotsAvailable != 0 || len(acceptedStatus.ActiveTasks) != 1 {
		t.Fatalf("incoherent accepted status: %+v", acceptedStatus)
	}
	release.Do(func() { close(executor.finish) })
	env, err = queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeSyncResult {
		t.Fatalf("result=%s err=%v", env.Type, err)
	}
	if client.TaskLimiter.InFlight() != 0 {
		t.Fatal("result delivered before slot release")
	}
	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_sync_task_results WHERE reported_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("result not durable: %d %v", pending, err)
	}
	env, err = queue.Dequeue(ctx)
	if err != nil || env.Type != protocolv2.TypeNodeStatus {
		t.Fatalf("completion capacity=%s err=%v", env.Type, err)
	}
	completedStatus, _ := protocolv2.Decode[protocolv2.NodeStatus](env)
	if completedStatus.CapacityRevision <= acceptedStatus.CapacityRevision || completedStatus.SyncTaskSlotsAvailable != 1 || len(completedStatus.ActiveTasks) != 0 {
		t.Fatalf("incoherent completion status: %+v", completedStatus)
	}
}

func TestV2ConcurrentCapacitySamplingAndAdmissionStayConsistent(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	executor := capacityFeedbackExecutor{started: make(chan protocol.SyncTask, 4), finish: make(chan struct{})}
	client := &Client{NodeID: "node-1", DB: db, V2Runtime: NewV2Runtime(), TaskLimiter: NewTaskLimiter(4), Executor: executor}
	queue := controlv2.NewQueue(64, 1<<20)
	defer queue.Close()
	defer func() {
		close(executor.finish)
		for i := 0; i < 4; i++ {
			client.waitV2Execution(fmt.Sprint(i), "attempt-1", time.Second)
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if err := client.enqueueV2Status(queue); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 4; i++ {
		if err := client.handleV2SyncTask(queue, capacityFeedbackTask(fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		env, err := queue.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if env.Type != protocolv2.TypeNodeStatus {
			continue
		}
		status, _ := protocolv2.Decode[protocolv2.NodeStatus](env)
		if status.CapacityRevision == 0 || status.SyncTaskSlotsAvailable+len(status.ActiveTasks) != 4 {
			t.Fatalf("torn capacity snapshot: %+v", status)
		}
		break
	}
}
