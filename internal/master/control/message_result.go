package control

func learnSyncTaskSlots(result *controlMessageResult, nodeID string, runtime *RuntimeStore) {
	if result == nil || !result.SyncTaskSlotsKnown || runtime == nil {
		return
	}
	runtime.SetSyncTaskSlotsAvailable(nodeID, result.SyncTaskSlotsAvailable)
}
