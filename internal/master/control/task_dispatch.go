package control

import "mirror-server/internal/protocol"

const maxSyncTasksPerControlSession = 10

func shouldDispatchNextTask(messageType string) bool {
	return messageType == protocol.TypeInventoryReport ||
		messageType == protocol.TypeSyncTaskAck ||
		messageType == protocol.TypeSyncTaskResult
}
