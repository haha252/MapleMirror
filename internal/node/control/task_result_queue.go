package control

import (
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const runningTaskAckCandidateBatch = 50

func (c Client) sendNextPendingTaskResult(conn net.Conn, reqID string,
	sequence uint64) (uint64, bool, error) {
	if c.DB == nil {
		return sequence, false, nil
	}
	results, err := c.loadPendingTaskResults(1)
	if err != nil || len(results) == 0 {
		return sequence, false, err
	}
	result := results[0]
	next, err := c.sendTaskResult(conn, reqID, sequence, result)
	if err != nil {
		return sequence, false, err
	}
	if _, err = c.DB.Exec(`UPDATE pending_sync_task_results SET reported_at = ?
		WHERE task_id = ?`, time.Now().UTC().Format(time.RFC3339Nano),
		result.TaskID); err != nil {
		return sequence, false, err
	}
	return next, true, nil
}

func (c Client) loadPendingTaskResults(limit int) ([]protocol.SyncTaskResult, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := c.DB.Query(`SELECT task_id, asset_id, result, COALESCE(local_digest_sha256, ''),
		size_bytes, COALESCE(message, ''), COALESCE(peer_fallback_attempted, 0)
		FROM pending_sync_task_results
		WHERE reported_at IS NULL ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []protocol.SyncTaskResult
	for rows.Next() {
		result, err := scanPendingTaskResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (c Client) sendNextRunningTaskAck(conn net.Conn, reqID string,
	sequence uint64) (uint64, bool, error) {
	if c.DB == nil {
		return sequence, false, nil
	}
	items, err := c.loadRunningTaskAcks(c.runningTaskAckCandidateLimit())
	if err != nil || len(items) == 0 {
		return sequence, false, err
	}
	ack, ok := c.nextRunningTaskAckDue(items, time.Now())
	if !ok {
		return sequence, false, nil
	}
	next, err := c.sendRunningTaskAck(conn, reqID, sequence, ack)
	if err != nil {
		return sequence, false, err
	}
	now := time.Now()
	if err := c.markRunningTaskAckSent(ack.TaskID, now); err != nil {
		return sequence, false, err
	}
	return next, true, nil
}

func (c Client) runningTaskAckCandidateLimit() int {
	if len(c.runningTaskAckSent) == 0 {
		return runningTaskAckCandidateBatch
	}
	return len(c.runningTaskAckSent) + runningTaskAckCandidateBatch
}

func (c Client) loadRunningTaskAcks(limit int) ([]protocol.SyncTaskAck, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := c.DB.Query(`SELECT task_id
		FROM local_sync_tasks WHERE state = 'running' ORDER BY updated_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []protocol.SyncTaskAck
	for rows.Next() {
		var ack protocol.SyncTaskAck
		if err := rows.Scan(&ack.TaskID); err != nil {
			return nil, err
		}
		ack.State = "running"
		ack.Message = "任务仍在执行"
		items = append(items, ack)
	}
	return items, rows.Err()
}

func (c *Client) nextRunningTaskAckDue(items []protocol.SyncTaskAck,
	now time.Time) (protocol.SyncTaskAck, bool) {
	for _, ack := range items {
		if c.runningTaskAckDue(ack.TaskID, now) {
			return ack, true
		}
	}
	return protocol.SyncTaskAck{}, false
}

func (c *Client) runningTaskAckDue(taskID string, now time.Time) bool {
	if c.runningTaskAckSent == nil {
		c.runningTaskAckSent = map[string]time.Time{}
	}
	last, ok := c.runningTaskAckSent[taskID]
	return !ok || now.Sub(last) >= runningTaskAckLogInterval
}

func (c *Client) markRunningTaskAckSent(taskID string, now time.Time) error {
	if c.runningTaskAckSent == nil {
		c.runningTaskAckSent = map[string]time.Time{}
	}
	c.runningTaskAckSent[taskID] = now
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`UPDATE local_sync_tasks SET updated_at = ?
		WHERE task_id = ? AND state = 'running'`,
		now.UTC().Format(time.RFC3339Nano), taskID)
	return err
}
