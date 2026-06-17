package control

import "mirror-server/internal/protocol"

const (
	defaultSyncTaskDispatchWindow = 1
	maxSyncTaskDispatchWindow     = 64
)

func shouldDispatchNextTask(messageType string) bool {
	return messageType == protocol.TypeHeartbeat ||
		messageType == protocol.TypePressureReport ||
		messageType == protocol.TypeInventoryReport ||
		messageType == protocol.TypeSyncTaskAck ||
		messageType == protocol.TypeSyncTaskResult
}
