package control

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestTaskAckMessageIDIncludesTaskID(t *testing.T) {
	db := openNodeDB(t)
	defer db.Close()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	seen := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, want := range []string{"task-1", "task-2"} {
			ack, ok := expectType(t, server, protocol.TypeSyncTaskAck)
			if !ok {
				return
			}
			if !strings.Contains(ack.MessageID, want) {
				t.Errorf("ack message id %q does not include task id %q", ack.MessageID, want)
				return
			}
			seen <- ack.MessageID
			sendAck(server, ack)
		}
	}()
	ctl := Client{
		NodeID:      "node-1",
		DB:          db,
		Executor:    recordingExecutor{tasks: make(chan string, 2)},
		TaskLimiter: NewTaskLimiter(2),
	}
	sequence := uint64(3)
	for _, taskID := range []string{"task-1", "task-2"} {
		body, _ := json.Marshal(protocol.SyncTask{
			TaskID: taskID, TaskType: "asset_download",
			Asset: protocol.SyncAsset{AssetID: "asset-1"},
		})
		next, err := ctl.handleDispatchedTask(client, "req-1", sequence, protocol.Envelope{
			ProtocolVersion: protocol.Version,
			MessageID:       taskID,
			MessageType:     protocol.TypeSyncTask,
			SentAt:          time.Now().UTC(),
			NodeID:          "node-1",
			RequestID:       "req-1",
			Payload:         body,
		})
		if err != nil {
			t.Fatal(err)
		}
		sequence = next
	}
	<-done
	first := <-seen
	second := <-seen
	if first == second {
		t.Fatalf("ack message ids should differ, got %q", first)
	}
}
