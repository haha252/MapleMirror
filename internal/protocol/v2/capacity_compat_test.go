package v2

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapacityFeedbackFieldsAreOptionalForExistingNodes(t *testing.T) {
	var status NodeStatus
	if err := json.Unmarshal([]byte(`{"sync_task_slots_available":2,"active_tasks":[{"task_id":"t","attempt_id":"a"}]}`), &status); err != nil {
		t.Fatal(err)
	}
	if status.CapacityRevision != 0 || status.SyncTaskSlotsAvailable != 2 || len(status.ActiveTasks) != 1 {
		t.Fatalf("legacy status not decoded: %+v", status)
	}
	var rejected SyncRejected
	if err := json.Unmarshal([]byte(`{"task_id":"t","attempt_id":"a","reason":"sync task slots full"}`), &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Code != "" || rejected.CapacityRevision != 0 {
		t.Fatalf("legacy rejection acquired a code/version: %+v", rejected)
	}
	for _, value := range []any{status, rejected} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "capacity_revision") || strings.Contains(string(encoded), `"code"`) {
			t.Fatalf("optional fields were not omitted: %s", encoded)
		}
	}
}

func TestCapacityFeedbackPreservesLegacyReceiverContract(t *testing.T) {
	encoded, err := json.Marshal(SyncRejected{TaskID: "t", AttemptID: "a", Reason: "sync task slots full", Code: SyncRejectedSlotsFull, CapacityRevision: 7})
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		TaskID    string `json:"task_id"`
		AttemptID string `json:"attempt_id"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.TaskID != "t" || legacy.AttemptID != "a" || legacy.Reason != "sync task slots full" {
		t.Fatalf("legacy contract changed: %+v", legacy)
	}
	var current SyncRejected
	if err := json.Unmarshal(encoded, &current); err != nil {
		t.Fatal(err)
	}
	if current.Code != SyncRejectedSlotsFull || current.CapacityRevision != 7 {
		t.Fatalf("new contract lost feedback: %+v", current)
	}
}
