package control

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (c *Client) storePendingV2Result(result protocolv2.SyncResult) error {
	if c.DB == nil {
		return nil
	}
	_, err := c.DB.Exec(`INSERT INTO pending_sync_task_results
		(task_id, asset_id, result, local_digest_sha256, size_bytes, message,
		peer_fallback_attempted, created_at, reported_at, attempt_id)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, NULL, ?)
		ON CONFLICT(task_id) DO UPDATE SET asset_id=excluded.asset_id, result=excluded.result,
		local_digest_sha256=excluded.local_digest_sha256, size_bytes=excluded.size_bytes,
		message=excluded.message, created_at=excluded.created_at, reported_at=NULL, attempt_id=excluded.attempt_id`,
		result.TaskID, result.AssetID, result.Result, nullableString(result.LocalDigestSHA256), result.SizeBytes,
		nullableString(result.Message), time.Now().UTC().Format(time.RFC3339Nano), result.AttemptID)
	if err != nil {
		return err
	}
	_, err = c.DB.Exec(`UPDATE local_sync_tasks SET state=?, error_message=?, updated_at=?
		WHERE task_id=? AND attempt_id=?`, result.Result, nullableString(result.Message),
		time.Now().UTC().Format(time.RFC3339Nano), result.TaskID, result.AttemptID)
	return err
}

func (c *Client) enqueuePendingV2Results(queue *controlv2.Queue) error {
	if c.DB == nil {
		return nil
	}
	rows, err := c.DB.Query(`SELECT task_id, COALESCE(attempt_id,''), COALESCE(asset_id,''), result,
		COALESCE(local_digest_sha256,''), size_bytes, COALESCE(message,'')
		FROM pending_sync_task_results WHERE reported_at IS NULL ORDER BY created_at LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var result protocolv2.SyncResult
		if err := rows.Scan(&result.TaskID, &result.AttemptID, &result.AssetID, &result.Result,
			&result.LocalDigestSHA256, &result.SizeBytes, &result.Message); err != nil {
			return err
		}
		if result.AttemptID == "" { // legacy pending result is delivered by control.v1 only.
			continue
		}
		id := protocolv2.StableMessageID(protocolv2.TypeSyncResult, result.TaskID, result.AttemptID)
		envelope, _ := protocolv2.New(protocolv2.TypeSyncResult, id, result)
		if err := queue.Enqueue(envelope, ""); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (c *Client) handleV2ResultAck(envelope protocolv2.Envelope) error {
	ack, err := protocolv2.Decode[protocolv2.SyncResultAck](envelope)
	if err != nil {
		return err
	}
	if ack.TaskID == "" || ack.AttemptID == "" {
		return errors.New("sync.result.ack missing task identity")
	}
	expectedReplyTo := protocolv2.StableMessageID(protocolv2.TypeSyncResult, ack.TaskID, ack.AttemptID)
	if envelope.ReplyTo != expectedReplyTo {
		return fmt.Errorf("sync.result.ack reply_to mismatch")
	}
	if c.DB == nil {
		return nil
	}
	res, err := c.DB.Exec(`UPDATE pending_sync_task_results SET reported_at=?
		WHERE task_id=? AND attempt_id=? AND reported_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), ack.TaskID, ack.AttemptID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var reportedAt string
		err := c.DB.QueryRow(`SELECT COALESCE(reported_at,'') FROM pending_sync_task_results
			WHERE task_id=? AND attempt_id=?`, ack.TaskID, ack.AttemptID).Scan(&reportedAt)
		if err == sql.ErrNoRows {
			return fmt.Errorf("sync.result.ack does not match pending result")
		}
		if err != nil {
			return err
		}
		if reportedAt != "" {
			return nil // duplicate ACK for an already confirmed durable result.
		}
		return fmt.Errorf("sync.result.ack does not match pending result")
	}
	return nil
}

func (c *Client) loadV2ActiveTasks() []protocolv2.ActiveTask {
	if c.DB == nil {
		return nil
	}
	rows, err := c.DB.Query(`SELECT task_id, COALESCE(attempt_id,'') FROM local_sync_tasks
		WHERE state IN ('running','waiting_manifest') AND COALESCE(attempt_id,'') != '' ORDER BY task_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []protocolv2.ActiveTask
	for rows.Next() {
		var item protocolv2.ActiveTask
		if rows.Scan(&item.TaskID, &item.AttemptID) == nil {
			out = append(out, item)
		}
	}
	return out
}
