package control

import (
	"encoding/json"
	"time"

	"mirror-server/internal/protocol"
)

func fitSyncTaskForControlFrame(task protocol.SyncTask, session Session, reqID string) protocol.SyncTask {
	for !syncTaskFitsControlFrame(task, session, reqID) {
		if !shrinkSyncTaskFallback(&task) {
			return task
		}
	}
	return task
}

func syncTaskFitsControlFrame(task protocol.SyncTask, session Session, reqID string) bool {
	body, err := json.Marshal(task)
	if err != nil {
		return false
	}
	envelope, err := json.Marshal(protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: task.TaskID,
		MessageType: protocol.TypeSyncTask, SentAt: time.Now().UTC(),
		NodeID: session.NodeID, RequestID: reqID, Payload: body,
	})
	return err == nil && len(envelope) <= protocol.MaxFrameBytes
}

func shrinkSyncTaskFallback(task *protocol.SyncTask) bool {
	for i := range task.FallbackSources {
		if len(task.FallbackSources[i].Parts) > 0 {
			task.FallbackSources[i].Parts = nil
			return true
		}
	}
	if len(task.FallbackSources) > 0 {
		task.FallbackSources = task.FallbackSources[:len(task.FallbackSources)-1]
		return true
	}
	return false
}
