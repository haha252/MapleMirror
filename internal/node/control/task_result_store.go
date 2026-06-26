package control

import (
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"

	"mirror-server/internal/protocol"
)

const pendingTaskResultStoreAttempts = 5
const maxSyncTaskResultMessageBytes = 16 * 1024

func (c *Client) storePendingTaskResultWithRetry(result protocol.SyncTaskResult) error {
	var err error
	for attempt := 1; attempt <= pendingTaskResultStoreAttempts; attempt++ {
		err = c.storePendingTaskResult(result)
		if err == nil {
			return nil
		}
		if !retryableSQLiteError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt*25) * time.Millisecond)
	}
	return err
}

func retryableSQLiteError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "busy") ||
		strings.Contains(message, "locked")
}

func (c *Client) storePendingTaskResult(result protocol.SyncTaskResult) error {
	if c.DB == nil {
		return nil
	}
	result.Message = trimSyncTaskResultMessage(result.Message)
	tx, err := c.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message,
		peer_fallback_attempted, created_at, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(task_id) DO UPDATE SET asset_id = excluded.asset_id,
		result = excluded.result, local_digest_sha256 = excluded.local_digest_sha256,
		size_bytes = excluded.size_bytes, message = excluded.message,
		peer_fallback_attempted = excluded.peer_fallback_attempted,
		created_at = excluded.created_at, reported_at = NULL`,
		result.TaskID, result.AssetID, result.Result, nullableString(result.LocalDigestSHA256),
		result.SizeBytes, nullableString(result.Message), boolInt(result.PeerFallbackAttempted),
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err := recordLocalTaskResult(tx, result); err != nil {
		return err
	}
	return tx.Commit()
}

func trimSyncTaskResultMessage(message string) string {
	if len(message) <= maxSyncTaskResultMessageBytes {
		return message
	}
	out := message[:maxSyncTaskResultMessageBytes]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out + "...(truncated)"
}

func recordLocalTaskResult(tx *sql.Tx, result protocol.SyncTaskResult) error {
	state := result.Result
	switch result.Result {
	case "succeeded":
		state = "succeeded"
	case "temporary_error", "digest_mismatch", "size_mismatch":
		state = "failed"
	default:
		if state == "" || state == "running" {
			state = "failed"
		}
	}
	_, err := tx.Exec(`UPDATE local_sync_tasks SET state = ?,
		error_message = ?, updated_at = ? WHERE task_id = ?`,
		state, nullableString(result.Message), time.Now().UTC().Format(time.RFC3339Nano),
		result.TaskID)
	if err != nil {
		return err
	}
	if result.Result != "succeeded" {
		return nil
	}
	taskType, err := localTaskType(tx, result.TaskID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if taskType != "inventory_reconcile" {
		return nil
	}
	return forceInventoryReportDue(tx)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
