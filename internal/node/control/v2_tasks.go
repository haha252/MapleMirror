package control

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

type v2TaskExecutor interface {
	ExecuteV2(context.Context, protocolv2.SyncTask) (protocolv2.SyncResult, *protocolv2.SwarmManifest)
}

func (c *Client) handleV2SyncTask(queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	task, err := protocolv2.Decode[protocolv2.SyncTask](envelope)
	if err != nil {
		return err
	}
	if task.TaskID == "" || task.AttemptID == "" {
		return errors.New("sync.task missing task_id/attempt_id")
	}
	if envelope.ID != protocolv2.StableMessageID(protocolv2.TypeSyncTask, task.TaskID, task.AttemptID) {
		return errors.New("sync.task message id mismatch")
	}
	if task.Manifest != nil && c.Swarm != nil {
		c.Swarm.SetManifest(*task.Manifest)
		c.Swarm.SetSources(task.Manifest.ManifestID, task.Sources)
	}
	if c.DB == nil || c.Executor == nil {
		return c.enqueueV2Rejected(queue, envelope.ID, task, "sync executor unavailable")
	}
	var existingAttempt, state string
	err = c.DB.QueryRow(`SELECT COALESCE(attempt_id,''), state FROM local_sync_tasks WHERE task_id=?`, task.TaskID).
		Scan(&existingAttempt, &state)
	if err == nil && existingAttempt == task.AttemptID {
		if (state == "running" && c.V2Runtime.hasExecution(task.TaskID, task.AttemptID)) || (state != "cancelled" && c.hasV2DurableTask(task.TaskID, task.AttemptID)) {
			if err := c.enqueueV2Accepted(queue, envelope.ID, task); err != nil {
				return err
			}
			return c.enqueueV2Durable(queue)
		}
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if existingAttempt != "" && existingAttempt != task.AttemptID {
		c.cancelV2Execution(task.TaskID, existingAttempt)
		if !c.waitV2Execution(task.TaskID, existingAttempt, 2*time.Second) {
			return c.enqueueV2Rejected(queue, envelope.ID, task, "previous attempt is still stopping")
		}
	}
	if !c.reserveSyncTaskSlot() {
		return c.enqueueV2Rejected(queue, envelope.ID, task, "sync task slots full")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = c.DB.Exec(`INSERT INTO local_sync_tasks
		(task_id, asset_id, task_type, state, error_message, updated_at, attempt_id)
		VALUES (?, ?, ?, 'running', NULL, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET asset_id=excluded.asset_id, task_type=excluded.task_type,
		state='running', error_message=NULL, updated_at=excluded.updated_at, attempt_id=excluded.attempt_id`,
		task.TaskID, task.Asset.AssetID, task.TaskType, now, task.AttemptID)
	if err != nil {
		c.releaseSyncTaskSlot()
		return err
	}
	if err := c.enqueueV2Accepted(queue, envelope.ID, task); err != nil {
		c.releaseSyncTaskSlot()
		// The worker has not started. Persist a retryable result so reconnects
		// cannot mistake this row for an executing attempt and keep its lease.
		persistErr := c.storePendingV2Result(protocolv2.SyncResult{
			TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: task.Asset.AssetID,
			Result: "temporary_error", Message: "sync task acceptance could not be queued: " + err.Error(),
		})
		return errors.Join(err, persistErr)
	}
	c.executeV2TaskAsync(queue, task)
	return nil
}

func (c *Client) enqueueV2Accepted(queue *controlv2.Queue, replyTo string, task protocolv2.SyncTask) error {
	id, _ := requestid.New()
	envelope, _ := protocolv2.Reply(protocolv2.TypeSyncAccepted, id, replyTo,
		protocolv2.SyncAccepted{TaskID: task.TaskID, AttemptID: task.AttemptID})
	return queue.Enqueue(envelope, task.TaskID+"/"+task.AttemptID)
}

func (c *Client) enqueueV2Rejected(queue *controlv2.Queue, replyTo string, task protocolv2.SyncTask, reason string) error {
	id, _ := requestid.New()
	envelope, _ := protocolv2.Reply(protocolv2.TypeSyncRejected, id, replyTo,
		protocolv2.SyncRejected{TaskID: task.TaskID, AttemptID: task.AttemptID, Reason: reason})
	return queue.Enqueue(envelope, task.TaskID+"/"+task.AttemptID)
}

func (c *Client) executeV2TaskAsync(queue *controlv2.Queue, task protocolv2.SyncTask) {
	ctx, cancel := context.WithTimeout(context.Background(), c.syncTaskTimeout())
	runtime := c.v2Runtime()
	done := make(chan struct{})
	runtime.mu.Lock()
	if old, ok := runtime.executions[task.TaskID]; ok && old.cancel != nil {
		old.cancel()
	}
	runtime.executions[task.TaskID] = v2Execution{attemptID: task.AttemptID, cancel: cancel, done: done, ctx: ctx}
	runtime.mu.Unlock()
	go func() {
		defer cancel()
		defer func() {
			c.releaseSyncTaskSlot()
			close(done)
			_ = c.enqueueV2Status(queue)
			if c.EventWake != nil {
				c.EventWake.Wake()
			}
		}()
		var result protocolv2.SyncResult
		var manifest *protocolv2.SwarmManifest
		runErr := c.TaskLimiter.Run(ctx, func() {
			if executor, ok := c.Executor.(v2TaskExecutor); ok {
				result, manifest = executor.ExecuteV2(ctx, task)
				return
			}
			legacyTask := protocol.SyncTask{TaskID: task.TaskID, TaskType: task.TaskType, Asset: protocol.SyncAsset{
				AssetID: task.Asset.AssetID, ProjectID: task.Asset.ProjectID, Version: task.Asset.Version,
				FileName: task.Asset.FileName, SizeBytes: task.Asset.SizeBytes, DownloadURL: task.Asset.DownloadURL, DigestSHA256: task.Asset.DigestSHA256}}
			legacy := c.Executor.Execute(ctx, legacyTask)
			result = protocolv2.SyncResult{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: legacy.AssetID, Result: legacy.Result, LocalDigestSHA256: legacy.LocalDigestSHA256, SizeBytes: legacy.SizeBytes, Message: trimSyncTaskResultMessage(legacy.Message)}
		})
		if runErr != nil || (result.Result == "" && ctx.Err() != nil) {
			result = protocolv2.SyncResult{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: task.Asset.AssetID, Result: "temporary_error", Message: "sync task cancelled or timed out"}
			manifest = nil
		}
		if manifest != nil && result.Result == "succeeded" {
			if c.Swarm != nil {
				c.Swarm.SetManifest(*manifest)
			}
			if err := c.storePendingV2Manifest(*manifest, result); err != nil {
				result.Result = "temporary_error"
				result.Message = "persist swarm manifest outbox: " + err.Error()
				if c.storePendingV2Result(result) == nil {
					_ = c.enqueuePendingV2Results(queue)
				}
			} else {
				_ = c.enqueuePendingV2Manifests(queue)
			}
		} else if err := c.storePendingV2Result(result); err == nil {
			_ = c.enqueuePendingV2Results(queue)
		}
		runtime.mu.Lock()
		if current, ok := runtime.executions[task.TaskID]; ok && current.attemptID == task.AttemptID {
			delete(runtime.executions, task.TaskID)
		}
		runtime.mu.Unlock()
	}()
}

func (c *Client) cancelV2Execution(taskID, attemptID string) {
	runtime := c.v2Runtime()
	runtime.mu.Lock()
	execution, ok := runtime.executions[taskID]
	if !ok || (attemptID != "" && execution.attemptID != attemptID) {
		ok = false
	}
	runtime.mu.Unlock()
	if ok && execution.cancel != nil {
		execution.cancel()
	}
}

func (c *Client) handleV2Cancel(envelope protocolv2.Envelope) error {
	cancel, err := protocolv2.Decode[protocolv2.SyncCancel](envelope)
	if err != nil {
		return err
	}
	if cancel.TaskID == "" {
		return errors.New("sync.cancel missing task_id")
	}
	if cancel.AttemptID != "" && c.DB != nil {
		var current string
		if err := c.DB.QueryRow(`SELECT COALESCE(attempt_id,'') FROM local_sync_tasks WHERE task_id=?`, cancel.TaskID).Scan(&current); err == nil && current != cancel.AttemptID {
			return nil
		}
	}
	c.cancelV2Execution(cancel.TaskID, cancel.AttemptID)
	if c.DB != nil {
		_, _ = c.DB.Exec(`UPDATE local_sync_tasks SET state='cancelled', error_message=?, updated_at=? WHERE task_id=?`,
			nullableString(cancel.Reason), time.Now().UTC().Format(time.RFC3339Nano), cancel.TaskID)
	}
	return nil
}
