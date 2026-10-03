package control

import (
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func (c *Client) hasV2DurableTask(taskID, attemptID string) bool {
	var present int
	err := c.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pending_swarm_manifests WHERE task_id=? AND attempt_id=?)
	 OR EXISTS(SELECT 1 FROM pending_sync_task_results WHERE task_id=? AND attempt_id=? AND reported_at IS NULL)`,
		taskID, attemptID, taskID, attemptID).Scan(&present)
	return err == nil && present == 1
}

func (c *Client) storePendingV2Result(result protocolv2.SyncResult) error {
	var err error
	for attempt := 1; attempt <= pendingTaskResultStoreAttempts; attempt++ {
		err = c.storePendingV2ResultOnce(result)
		if err == nil || !retryableSQLiteError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt*25) * time.Millisecond)
	}
	return err
}
