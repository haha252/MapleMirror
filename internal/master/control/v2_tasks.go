package control

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (r Repository) nextV2SyncTask(ctx context.Context, nodeID string) (protocolv2.SyncTask, bool, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return protocolv2.SyncTask{}, false, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)

	// A sent-but-not-accepted attempt is retransmitted with the same identity.
	var taskID, attemptID string
	err = tx.QueryRowContext(ctx, `SELECT id, attempt_id FROM node_tasks
		WHERE node_id = ? AND state = 'sent' AND attempt_id != ''
		AND lease_expires_at IS NOT NULL AND lease_expires_at > ?
		ORDER BY updated_at LIMIT 1`, nodeID, nowText).Scan(&taskID, &attemptID)
	if err != nil && err != sql.ErrNoRows {
		return protocolv2.SyncTask{}, false, err
	}
	if err == sql.ErrNoRows {
		attemptID, err = newID()
		if err != nil {
			return protocolv2.SyncTask{}, false, err
		}
		lease := now.Add(syncTaskLeaseDuration).Format(time.RFC3339Nano)
		capacityBudget := r.v2CapacityBudget(nodeID)
		maxSwarmAssetBytes := swarm.ProtocolMaxPieceSize * swarm.MaxPieces
		peerOnly := r.runtime().PeerOnly(nodeID)
		err = tx.QueryRowContext(ctx, `WITH candidate AS (
			SELECT t.id FROM node_tasks t
			LEFT JOIN assets a ON a.id = t.asset_id
			LEFT JOIN releases r ON r.id = a.release_id
			WHERE t.node_id = ? AND (
				t.state = 'pending' OR
				(t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?)) OR
				(t.state IN ('sent','running') AND (t.lease_expires_at IS NULL OR t.lease_expires_at = '' OR t.lease_expires_at <= ?))
			)
			AND (t.task_type != 'asset_download' OR COALESCE(a.size_bytes,0) <= ?)
			AND (t.task_type != 'asset_download'
				OR COALESCE(a.size_bytes,0) > ?
				OR EXISTS(SELECT 1 FROM asset_piece_manifests m WHERE m.asset_id=t.asset_id AND m.status IN ('authoritative','conflict','disabled'))
				OR (? = 0 AND NOT EXISTS(SELECT 1 FROM node_tasks seed WHERE seed.asset_id=t.asset_id AND seed.id!=t.id
					AND seed.task_type='asset_download' AND seed.state IN ('sent','running')
					AND seed.lease_expires_at IS NOT NULL AND seed.lease_expires_at>?)))`+eligibleSyncTaskSQL("t")+`
			ORDER BY r.published_at DESC, a.size_bytes, t.created_at LIMIT 1
		)
		UPDATE node_tasks SET state='sent', attempt_id=?, lease_expires_at=?, updated_at=?
		WHERE id=(SELECT id FROM candidate) AND node_id=?
		RETURNING id`, nodeID, nowText, nowText, capacityBudget, maxSwarmAssetBytes, peerOnly, nowText, attemptID, lease, nowText, nodeID).Scan(&taskID)
		if err == sql.ErrNoRows {
			return protocolv2.SyncTask{}, false, nil
		}
		if err != nil {
			return protocolv2.SyncTask{}, false, err
		}
	}
	task, err := loadV2Task(ctx, tx, nodeID, taskID, attemptID)
	if err != nil {
		return protocolv2.SyncTask{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return protocolv2.SyncTask{}, false, err
	}
	if task.TaskType == "asset_download" {
		if task.Asset.SizeBytes > swarm.ProtocolMaxPieceSize*swarm.MaxPieces {
			task.SwarmDisabled = true
			task.WholeSources = r.v2WholeSources(ctx, nodeID, task)
			return task, true, nil
		}
		status, found, err := r.v2ManifestStatus(ctx, task.Asset.AssetID)
		if err != nil {
			return protocolv2.SyncTask{}, false, err
		}
		switch {
		case found && (status == "conflict" || status == "disabled"):
			task.SwarmDisabled = true
			task.WholeSources = r.v2WholeSources(ctx, nodeID, task)
		case found && status == "authoritative":
			manifest, ok, err := r.LoadV2Manifest(ctx, task.Asset.AssetID)
			if err != nil {
				return protocolv2.SyncTask{}, false, err
			}
			if !ok {
				return protocolv2.SyncTask{}, false, errors.New("authoritative swarm manifest disappeared")
			}
			task.Manifest = &manifest
			task.Sources, err = r.v2SwarmSources(ctx, nodeID, manifest)
			if err != nil {
				return protocolv2.SyncTask{}, false, err
			}
		default:
			task.Bootstrap = true
		}
	}
	return task, true, nil
}

func loadV2Task(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID, taskID, attemptID string) (protocolv2.SyncTask, error) {
	var task protocolv2.SyncTask
	var assetID, projectID, version, fileName, sourceURL, digest sql.NullString
	var size sql.NullInt64
	var lease string
	err := q.QueryRowContext(ctx, `SELECT t.task_type, t.asset_id, COALESCE(t.lease_expires_at,''),
		COALESCE(a.id,''), COALESCE(r.project_id,''), COALESCE(r.tag_name,''),
		COALESCE(a.file_name,''), COALESCE(a.size_bytes,0), COALESCE(a.source_url,''), COALESCE(a.digest_sha256,'')
		FROM node_tasks t LEFT JOIN assets a ON a.id=t.asset_id
		LEFT JOIN releases r ON r.id=a.release_id
		WHERE t.id=? AND t.node_id=?`, taskID, nodeID).Scan(
		&task.TaskType, &assetID, &lease, &assetID, &projectID, &version, &fileName, &size, &sourceURL, &digest)
	if err != nil {
		return protocolv2.SyncTask{}, err
	}
	task.TaskID, task.AttemptID = taskID, attemptID
	task.Asset = protocolv2.SyncAsset{AssetID: assetID.String, ProjectID: projectID.String,
		Version: version.String, FileName: fileName.String, SizeBytes: size.Int64,
		DownloadURL: sourceURL.String, DigestSHA256: digest.String}
	task.LeaseExpiresAt, _ = time.Parse(time.RFC3339Nano, lease)
	return task, nil
}

func (r Repository) acceptV2Task(ctx context.Context, session Session, taskID, attemptID string) (bool, error) {
	if taskID == "" || attemptID == "" {
		return false, errors.New("task_id/attempt_id required")
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state='running', lease_expires_at=?,
		error_message=NULL, retry_after=NULL, updated_at=?
		WHERE id=? AND node_id=? AND attempt_id=? AND state IN ('sent','running')`,
		time.Now().UTC().Add(syncTaskLeaseDuration).Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano), taskID, session.NodeID, attemptID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (r Repository) rejectV2Task(ctx context.Context, session Session, rejected protocolv2.SyncRejected) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE node_tasks SET state='retry_wait', attempts=COALESCE(attempts,0)+1,
		error_message=?, retry_after=?, lease_expires_at=NULL, updated_at=?
		WHERE id=? AND node_id=? AND attempt_id=? AND state IN ('sent','running')`,
		rejected.Reason, time.Now().UTC().Add(5*time.Second).Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano), rejected.TaskID, session.NodeID, rejected.AttemptID)
	return err
}

func (r Repository) refreshV2TaskLeases(ctx context.Context, nodeID string, active []protocolv2.ActiveTask) error {
	if len(active) == 0 {
		return nil
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lease := time.Now().UTC().Add(syncTaskLeaseDuration).Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range active {
		if item.TaskID == "" || item.AttemptID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state='running', lease_expires_at=?, updated_at=?
			WHERE id=? AND node_id=? AND attempt_id=? AND state IN ('sent','running')`,
			lease, now, item.TaskID, nodeID, item.AttemptID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const v2MasterCapacitySafetyBytes int64 = 256 << 20

func (r Repository) v2CapacityBudget(nodeID string) int64 {
	latest, ok := r.runtime().LatestV2Status(nodeID)
	if !ok || !latest.Status.AssetFS.Valid || !latest.Status.PartialFS.Valid {
		return math.MaxInt64
	}
	usable := func(fs protocolv2.FilesystemCapacity) int64 {
		value := fs.AvailableBytes - fs.ReservedBytes - v2MasterCapacitySafetyBytes
		if value < 0 {
			return 0
		}
		return value
	}
	asset := usable(latest.Status.AssetFS)
	partial := usable(latest.Status.PartialFS)
	if partial < asset {
		return partial
	}
	return asset
}

func (r Repository) v2DispatchAllowance(ctx context.Context, nodeID string) int {
	slots, known := r.runtime().SyncTaskDispatchCapacity(nodeID)
	if !known || slots <= 0 {
		return 0
	}
	var outstanding int
	_ = r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_tasks WHERE node_id=?
		AND state IN ('sent','running') AND lease_expires_at>?`, nodeID,
		time.Now().UTC().Format(time.RFC3339Nano)).Scan(&outstanding)
	reflected := 0
	if latest, ok := r.runtime().LatestV2Status(nodeID); ok {
		reflected = len(latest.Status.ActiveTasks)
	}
	unreflected := outstanding - reflected
	if unreflected < 0 {
		unreflected = 0
	}
	if slots <= unreflected {
		return 0
	}
	return slots - unreflected
}

func (s *V2Server) dispatchV2Tasks(ctx context.Context, session Session, queue *controlv2.Queue) error {
	allowance := s.Repo.v2DispatchAllowance(ctx, session.NodeID)
	for i := 0; i < allowance; i++ {
		task, ok, err := s.Repo.nextV2SyncTask(ctx, session.NodeID)
		if err != nil || !ok {
			return err
		}
		envelope, err := protocolv2.New(protocolv2.TypeSyncTask, protocolv2.StableMessageID(protocolv2.TypeSyncTask, task.TaskID, task.AttemptID), task)
		if err != nil {
			return err
		}
		if err := queue.Enqueue(envelope, task.TaskID+"/"+task.AttemptID); err != nil {
			return err
		}
	}
	return nil
}
