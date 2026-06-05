package control

import "mirror-server/internal/protocol"

func shouldDispatchNextTask(messageType string) bool {
	return messageType == protocol.TypeInventoryReport || messageType == protocol.TypeSyncTaskResult
}
