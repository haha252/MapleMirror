package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/storage"
)

func openNodeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type recordingExecutor struct {
	tasks chan string
}

func (e recordingExecutor) Execute(ctx context.Context, task protocol.SyncTask) protocol.SyncTaskResult {
	select {
	case e.tasks <- task.TaskID:
	case <-ctx.Done():
	}
	return protocol.SyncTaskResult{
		TaskID: task.TaskID, AssetID: task.Asset.AssetID, Result: "succeeded",
	}
}

func writeSyncTask(t *testing.T, conn net.Conn, taskID string) {
	t.Helper()
	writeSyncTaskFor(t, conn, protocol.Envelope{
		NodeID:    "node-1",
		RequestID: "req-1",
	}, taskID)
}

func writeSyncTaskFor(t *testing.T, conn net.Conn, msg protocol.Envelope, taskID string) {
	t.Helper()
	body, _ := json.Marshal(protocol.SyncTask{
		TaskID: taskID, TaskType: "asset_delete",
		Asset: protocol.SyncAsset{AssetID: "asset-" + taskID},
	})
	if err := protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       taskID,
		MessageType:     protocol.TypeSyncTask,
		SentAt:          time.Now().UTC(),
		NodeID:          msg.NodeID,
		RequestID:       msg.RequestID,
		Payload:         body,
	}); err != nil {
		t.Error(err)
	}
}
