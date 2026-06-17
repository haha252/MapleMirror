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

func TestHandleMessageDoesNotLearnMissingHeartbeatOrPressureSlots(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	control := ControlServer{Repo: repo}

	heartbeatBody := []byte(`{
		"status": "syncing",
		"uptime_seconds": 10,
		"active_downloads": 0,
		"free_bytes": 0,
		"pressure": {
			"target_bandwidth_bps": 0,
			"actual_bandwidth_bps": 0,
			"ratio": 0
		}
	}`)
	result, err := control.handleMessage(session, envelopeWithRawPayload(session, protocol.TypeHeartbeat, 1, heartbeatBody))
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncTaskSlotsKnown {
		t.Fatalf("old heartbeat without slots must not mark capacity known: %+v", result)
	}
	if _, known := repo.runtime().SyncTaskDispatchCapacity(session.NodeID); known {
		t.Fatal("old heartbeat should not overwrite runtime slot knowledge")
	}

	reportBody := []byte(`{
		"report_id": "pressure-old",
		"sampled_at": "2026-05-28T02:02:00Z",
		"sample_window_seconds": 10,
		"target_bandwidth_bps": 0,
		"actual_bandwidth_bps": 0,
		"pressure_ratio": 0,
		"active_downloads": 0,
		"free_bytes": 0,
		"max_mirror_projects": 0
	}`)
	result, err = control.handleMessage(session, envelopeWithRawPayload(session, protocol.TypePressureReport, 2, reportBody))
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncTaskSlotsKnown {
		t.Fatalf("old pressure report without slots must not mark capacity known: %+v", result)
	}
	if _, known := repo.runtime().SyncTaskDispatchCapacity(session.NodeID); known {
		t.Fatal("old pressure report should not overwrite runtime slot knowledge")
	}
}

func TestHandleMessageLearnsExplicitZeroHeartbeatOrPressureSlots(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	session := seedNodeAndSession(t, repo)
	control := ControlServer{Repo: repo}
	zero := 0

	result, err := control.handleMessage(session, envelopeWithPayload(t, session, protocol.TypeHeartbeat, 1, protocol.Heartbeat{
		Status: "syncing", SyncTaskSlotsAvailable: &zero,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 0 {
		t.Fatalf("explicit heartbeat zero slots not learned: %+v", result)
	}

	result, err = control.handleMessage(session, envelopeWithPayload(t, session, protocol.TypePressureReport, 2, protocol.PressureReport{
		ReportID: "pressure-zero", SampledAt: time.Now().UTC(), SampleWindowSeconds: 10,
		SyncTaskSlotsAvailable: &zero,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.SyncTaskSlotsKnown || result.SyncTaskSlotsAvailable != 0 {
		t.Fatalf("explicit pressure zero slots not learned: %+v", result)
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

func envelopeWithRawPayload(session Session, messageType string, sequence uint64, payload []byte) protocol.Envelope {
	return protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       "msg",
		MessageType:     messageType,
		SentAt:          time.Now().UTC(),
		NodeID:          session.NodeID,
		RequestID:       session.RequestID,
		Sequence:        sequence,
		Payload:         payload,
	}
}
