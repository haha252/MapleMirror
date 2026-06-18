package control

import (
	"net"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestDuplicateMessageRefreshesSlotsWithoutDispatch(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, updated_at)
		VALUES ('task-pending', ?, 'inventory_reconcile', 'pending', 'req', 'now', 'now')`,
		session.NodeID)
	if err := repo.runtime().UpdateSequence(session, 10); err != nil {
		t.Fatal(err)
	}
	repo.runtime().SetSyncTaskSlotsAvailable(session.NodeID, 0)

	slots := 5
	control := ControlServer{Repo: repo}
	result, err := control.handleMessage(session,
		envelopeWithPayload(t, session, protocol.TypeHeartbeat, 9, protocol.Heartbeat{
			Status: "syncing", SyncTaskSlotsAvailable: &slots,
		}))
	if err != nil {
		t.Fatal(err)
	}
	if result.DispatchSyncTasks || !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 5 {
		t.Fatalf("duplicate message should refresh slots without dispatch: %+v", result)
	}
	if got, known := repo.runtime().SyncTaskDispatchCapacity(session.NodeID); got != 5 || !known {
		t.Fatalf("duplicate slots not learned got=%d known=%v", got, known)
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	dispatched := make(chan int, 1)
	errs := make(chan error, 1)
	go func() {
		n, err := control.dispatchSyncTasksAfterMessage(serverConn, session, "req-1", result)
		dispatched <- n
		errs <- err
	}()
	select {
	case n := <-dispatched:
		if n != 0 {
			t.Fatalf("duplicate message dispatched %d tasks", n)
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("duplicate dispatch attempted to write a task")
	}
}
