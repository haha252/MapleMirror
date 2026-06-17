package control

import (
	"encoding/json"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestHandleMessageLearnsSlotsFromTaskResultAckAndInventory(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	seedAssetTarget(t, repo, session.NodeID)
	seedDownloadTask(t, repo, session.NodeID, "task-1", "asset-1", 0, "")
	markTaskRunning(t, repo, "task-1")
	control := ControlServer{Repo: repo}
	resultSlots := 3
	msg := envelopeWithPayload(t, session, protocol.TypeSyncTaskResult, 1, protocol.SyncTaskResult{
		TaskID: "task-1", AssetID: "asset-1", Result: "temporary_error",
		SyncTaskSlotsAvailable: &resultSlots,
	})
	result, err := control.handleMessage(session, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 3 {
		t.Fatalf("result slots not learned: %+v", result)
	}

	mustExecControl(t, repo.DB, `INSERT INTO node_tasks
		(id, node_id, task_type, state, request_id, created_at, updated_at)
		VALUES ('task-ack-slots', ?, 'inventory_reconcile', 'sent', 'req', 'now', 'now')`,
		session.NodeID)
	ackSlots := 2
	msg = envelopeWithPayload(t, session, protocol.TypeSyncTaskAck, 2, protocol.SyncTaskAck{
		TaskID: "task-ack-slots", State: "running", SyncTaskSlotsAvailable: &ackSlots,
	})
	result, err = control.handleMessage(session, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 2 {
		t.Fatalf("ack slots not learned: %+v", result)
	}

	inventorySlots := 4
	msg = envelopeWithPayload(t, session, protocol.TypeInventoryReport, 3, protocol.InventoryReport{
		ReportID: "inv-slots", Revision: 1, GeneratedAt: time.Now().UTC(),
		Complete: true, SyncTaskSlotsAvailable: &inventorySlots,
	})
	result, err = control.handleMessage(session, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 4 {
		t.Fatalf("inventory slots not learned: %+v", result)
	}
}

func envelopeWithPayload(t *testing.T, session Session, messageType string, sequence uint64, payload any) protocol.Envelope {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "msg",
		MessageType:     messageType,
		SentAt:          time.Now().UTC(),
		NodeID:          session.NodeID,
		RequestID:       session.RequestID,
		Sequence:        sequence,
		Payload:         body,
	}
}
