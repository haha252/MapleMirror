package control

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

func (c *Client) storePendingV2Manifest(manifest protocolv2.SwarmManifest, result protocolv2.SyncResult) error {
	if c.DB == nil {
		return errors.New("node database unavailable")
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = c.DB.Exec(`INSERT INTO pending_swarm_manifests
		(manifest_id,asset_id,task_id,attempt_id,manifest_json,result_json,created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(manifest_id) DO UPDATE SET asset_id=excluded.asset_id,
		task_id=excluded.task_id,attempt_id=excluded.attempt_id,
		manifest_json=excluded.manifest_json,result_json=excluded.result_json`,
		manifest.ManifestID, manifest.AssetID, result.TaskID, result.AttemptID,
		manifestJSON, resultJSON, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (c *Client) enqueuePendingV2Manifests(queue *controlv2.Queue) error {
	if c.DB == nil {
		return nil
	}
	rows, err := c.DB.Query(`SELECT manifest_id,manifest_json FROM pending_swarm_manifests ORDER BY created_at LIMIT 8`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var manifestID string
		var raw []byte
		if err := rows.Scan(&manifestID, &raw); err != nil {
			return err
		}
		var manifest protocolv2.SwarmManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return err
		}
		id := protocolv2.StableMessageID(protocolv2.TypeSwarmManifestReport, manifest.ManifestID)
		envelope, _ := protocolv2.New(protocolv2.TypeSwarmManifestReport, id, manifest)
		if err := queue.Enqueue(envelope, manifestID); err != nil && err != controlv2.ErrQueueFull {
			return err
		}
	}
	return rows.Err()
}

func (c *Client) handleV2ManifestAck(queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	ack, err := protocolv2.Decode[protocolv2.SwarmManifestAck](envelope)
	if err != nil {
		return err
	}
	if ack.ManifestID == "" {
		return errors.New("swarm manifest ack missing manifest_id")
	}
	expectedReplyTo := protocolv2.StableMessageID(protocolv2.TypeSwarmManifestReport, ack.ManifestID)
	if envelope.ReplyTo != expectedReplyTo {
		return errors.New("swarm manifest ack reply_to mismatch")
	}
	if c.DB == nil {
		return nil
	}
	var manifestRaw, resultRaw []byte
	err = c.DB.QueryRow(`SELECT manifest_json,result_json FROM pending_swarm_manifests WHERE manifest_id=?`, ack.ManifestID).Scan(&manifestRaw, &resultRaw)
	if err != nil {
		// A duplicate ACK after the outbox was already consumed is harmless.
		return nil
	}
	var manifest protocolv2.SwarmManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return err
	}
	if ack.AssetID == "" || ack.AssetID != manifest.AssetID {
		return errors.New("swarm manifest ack asset mismatch")
	}
	var result protocolv2.SyncResult
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return err
	}
	if ack.Status != "accepted" {
		result.Result = "temporary_error"
		result.Message = "manifest " + ack.Status + ": " + ack.Message
	}
	// Result durability must happen before deleting the manifest outbox. If the
	// process dies between these statements, replay is idempotent on task_id.
	if err := c.storePendingV2Result(result); err != nil {
		return err
	}
	if _, err := c.DB.Exec(`DELETE FROM pending_swarm_manifests WHERE manifest_id=?`, ack.ManifestID); err != nil {
		return err
	}
	return c.enqueuePendingV2Results(queue)
}

func (c *Client) handleV2Sources(envelope protocolv2.Envelope) error {
	body, err := protocolv2.Decode[protocolv2.SwarmSources](envelope)
	if err != nil {
		return err
	}
	if body.ManifestID == "" || body.TaskID == "" || body.AttemptID == "" || body.AssetID == "" {
		return errors.New("swarm.sources missing task/asset/manifest identity")
	}
	if envelope.ReplyTo != "" {
		expectedReplyTo := protocolv2.StableMessageID(protocolv2.TypeSwarmSourcesRequest, body.TaskID, body.AttemptID, body.AssetID, body.ManifestID)
		if envelope.ReplyTo != expectedReplyTo {
			return errors.New("swarm.sources reply_to mismatch")
		}
	}
	if c.DB != nil {
		var currentAttempt string
		err := c.DB.QueryRow(`SELECT COALESCE(attempt_id,'') FROM local_sync_tasks
			WHERE task_id=? AND asset_id=? AND state IN ('running','waiting_manifest')`, body.TaskID, body.AssetID).Scan(&currentAttempt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if currentAttempt != body.AttemptID {
			return nil
		}
	}
	if c.Swarm == nil {
		return nil
	}
	c.Swarm.SetSources(body.ManifestID, body.Sources)
	return nil
}

func (c *Client) enqueueV2SwarmState(queue *controlv2.Queue) error {
	if c.Swarm == nil {
		return nil
	}
	for _, p := range c.Swarm.Availabilities() {
		if p.ManifestID == "" || p.AssetID == "" {
			continue
		}
		id, _ := requestid.New()
		body := protocolv2.SwarmAvailability{AssetID: p.AssetID, ManifestID: p.ManifestID, Revision: p.Revision, Bitset: p.Bitset}
		env, _ := protocolv2.New(protocolv2.TypeSwarmAvailability, id, body)
		if err := queue.Enqueue(env, p.AssetID); err != nil && err != controlv2.ErrQueueFull {
			return err
		}
		if c.DB != nil && c.Swarm.NeedsSourceRefresh(p.ManifestID, 30*time.Second) {
			var taskID, attemptID string
			if err := c.DB.QueryRow(`SELECT task_id,COALESCE(attempt_id,'') FROM local_sync_tasks WHERE asset_id=? AND state IN ('running','waiting_manifest') ORDER BY updated_at DESC LIMIT 1`, p.AssetID).Scan(&taskID, &attemptID); err == nil && attemptID != "" {
				requestBody := protocolv2.SwarmSourcesRequest{TaskID: taskID, AttemptID: attemptID, AssetID: p.AssetID, ManifestID: p.ManifestID}
				rid := protocolv2.StableMessageID(protocolv2.TypeSwarmSourcesRequest, taskID, attemptID, p.AssetID, p.ManifestID)
				req, _ := protocolv2.New(protocolv2.TypeSwarmSourcesRequest, rid, requestBody)
				_ = queue.Enqueue(req, p.AssetID+"/"+attemptID)
			}
		}
	}
	return nil
}
