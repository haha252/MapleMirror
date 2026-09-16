package control

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/downloadurl"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

const swarmCapabilityTTL = 2 * time.Minute

func validateManifestStructure(m protocolv2.SwarmManifest) error {
	if m.AssetID == "" || m.ManifestID == "" || m.AssetSize < 0 || m.PieceSize <= 0 || m.PieceCount <= 0 || m.PieceCount > int(swarm.MaxPieces) {
		return fmt.Errorf("invalid manifest metadata")
	}
	if m.PieceLayoutVersion != swarm.LayoutVersion || m.PieceHashAlgorithm != "sha256" {
		return fmt.Errorf("unsupported manifest layout/hash")
	}
	if len(m.PieceHashes) != m.PieceCount*32 {
		return fmt.Errorf("manifest hash blob length mismatch")
	}
	wantCount := int((m.AssetSize + m.PieceSize - 1) / m.PieceSize)
	if m.AssetSize == 0 {
		wantCount = 1
	}
	if wantCount != m.PieceCount {
		return fmt.Errorf("manifest piece count mismatch")
	}
	if swarm.ManifestID(m.AssetID, m.AssetSHA256, m.AssetSize, m.PieceSize, m.PieceHashes) != m.ManifestID {
		return fmt.Errorf("manifest id mismatch")
	}
	return nil
}

func (r Repository) AcceptV2Manifest(ctx context.Context, nodeID string, m protocolv2.SwarmManifest) (protocolv2.SwarmManifestAck, error) {
	ack := protocolv2.SwarmManifestAck{ManifestID: m.ManifestID, AssetID: m.AssetID}
	if err := validateManifestStructure(m); err != nil {
		ack.Status = "rejected"
		ack.Message = err.Error()
		return ack, nil
	}
	var size int64
	var digest string
	if err := r.DB.QueryRowContext(ctx, `SELECT size_bytes,digest_sha256 FROM assets WHERE id=?`, m.AssetID).Scan(&size, &digest); err != nil {
		return ack, err
	}
	if size != m.AssetSize || digest != m.AssetSHA256 {
		ack.Status = "rejected"
		ack.Message = "manifest does not match authoritative asset"
		return ack, nil
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return ack, err
	}
	defer tx.Rollback()
	var existingID, status string
	err = tx.QueryRowContext(ctx, `SELECT id,status FROM asset_piece_manifests WHERE asset_id=?`, m.AssetID).Scan(&existingID, &status)
	if err == nil {
		if existingID == m.ManifestID && status == "authoritative" {
			ack.Status = "accepted"
			if err := tx.Commit(); err != nil {
				return ack, err
			}
			r.markV2SeedComplete(nodeID, m)
			return ack, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE asset_piece_manifests SET status='conflict',updated_at=? WHERE asset_id=?`, time.Now().UTC().Format(time.RFC3339Nano), m.AssetID); err != nil {
			return ack, err
		}
		ack.Status = "conflict"
		ack.Message = "manifest conflicts with authoritative manifest"
		return ack, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return ack, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO asset_piece_manifests
		(id,asset_id,asset_size,asset_sha256,piece_layout_version,piece_size,piece_count,piece_hash_blob,status,created_by_node_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?, 'authoritative',?,?,?)`, m.ManifestID, m.AssetID, m.AssetSize, m.AssetSHA256, m.PieceLayoutVersion, m.PieceSize, m.PieceCount, m.PieceHashes, nodeID, now, now)
	if err != nil {
		return ack, err
	}
	if err := tx.Commit(); err != nil {
		return ack, err
	}
	ack.Status = "accepted"
	r.markV2SeedComplete(nodeID, m)
	// Existing pending tasks for this asset can immediately become Swarm tasks.
	rows, _ := r.DB.QueryContext(ctx, `SELECT DISTINCT node_id FROM node_tasks WHERE asset_id=? AND state IN ('pending','retry_wait')`, m.AssetID)
	var nodes []string
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				nodes = append(nodes, id)
			}
		}
	}
	if len(nodes) > 0 {
		r.runtime().NotifySyncTasks(nodes...)
	}
	return ack, nil
}

func (r Repository) LoadV2Manifest(ctx context.Context, assetID string) (protocolv2.SwarmManifest, bool, error) {
	var m protocolv2.SwarmManifest
	err := r.DB.QueryRowContext(ctx, `SELECT id,asset_id,asset_size,asset_sha256,piece_layout_version,piece_size,piece_count,piece_hash_blob
		FROM asset_piece_manifests WHERE asset_id=? AND status='authoritative'`, assetID).Scan(&m.ManifestID, &m.AssetID, &m.AssetSize, &m.AssetSHA256, &m.PieceLayoutVersion, &m.PieceSize, &m.PieceCount, &m.PieceHashes)
	if err == sql.ErrNoRows {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.PieceHashAlgorithm = "sha256"
	return m, true, nil
}

func (r Repository) v2SwarmSources(ctx context.Context, targetNodeID string, m protocolv2.SwarmManifest) ([]protocolv2.SwarmSource, error) {
	partial := r.runtime().swarmAvailability(m.AssetID, m.ManifestID, targetNodeID)
	rows, err := r.DB.QueryContext(ctx, `SELECT n.id,n.public_download_base_url,
		CASE WHEN ni.state='verified' AND ni.local_digest_sha256=a.digest_sha256 AND ni.size_bytes=a.size_bytes THEN 1 ELSE 0 END
		FROM nodes n LEFT JOIN node_inventory ni ON ni.node_id=n.id AND ni.asset_id=?
		JOIN assets a ON a.id=? WHERE n.id!=? AND n.state NOT IN ('disabled','offline') AND n.public_download_base_url!=''`, m.AssetID, m.AssetID, targetNodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	expires := time.Now().UTC().Add(swarmCapabilityTTL).Format(time.RFC3339Nano)
	var out []protocolv2.SwarmSource
	for rows.Next() {
		var nodeID, base string
		var complete int
		if err := rows.Scan(&nodeID, &base, &complete); err != nil {
			return nil, err
		}
		bits, hasPartial := partial[nodeID]
		if complete == 0 && !hasPartial {
			continue
		}
		token, err := r.ReplicationSigner.SignSwarm(downloadtoken.SwarmClaims{AssetID: m.AssetID, ManifestID: m.ManifestID, SourceNodeID: nodeID, TargetNodeID: targetNodeID, ExpiresAt: expires, Scope: "swarm_piece_read", AssetSize: m.AssetSize, PieceSize: m.PieceSize, PieceCount: m.PieceCount})
		if err != nil {
			continue
		}
		baseURL, err := downloadurl.Join(base, "/internal/swarm/"+url.PathEscape(m.AssetID))
		if err != nil {
			continue
		}
		if complete != 0 {
			bits = make([]byte, swarm.BitsetBytes(m.PieceCount))
			for i := 0; i < m.PieceCount; i++ {
				swarm.Set(bits, i)
			}
		}
		out = append(out, protocolv2.SwarmSource{NodeID: nodeID, BaseURL: baseURL, Capability: token, Availability: bits, Complete: complete != 0})
	}
	return out, nil
}

type activeV2SwarmTask struct {
	TaskID    string
	NodeID    string
	AttemptID string
}

func (r Repository) activeV2SwarmTasks(ctx context.Context, assetID string) ([]activeV2SwarmTask, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id,node_id,COALESCE(attempt_id,'') FROM node_tasks
		WHERE asset_id=? AND state IN ('sent','running') AND COALESCE(attempt_id,'')!=''`, assetID)
	if err != nil {
		return nil, err
	}
	var tasks []activeV2SwarmTask
	for rows.Next() {
		var item activeV2SwarmTask
		if err := rows.Scan(&item.TaskID, &item.NodeID, &item.AttemptID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *V2Server) pushV2SourcesForAsset(ctx context.Context, assetID string) {
	manifest, ok, err := s.Repo.LoadV2Manifest(ctx, assetID)
	if err != nil || !ok {
		return
	}
	tasks, err := s.Repo.activeV2SwarmTasks(ctx, assetID)
	if err != nil {
		return
	}
	// The master database intentionally has MaxOpenConns(1). All task rows must
	// be drained and closed before v2SwarmSources performs its own DB query.
	for _, task := range tasks {
		qv, ok := s.queues.Load(task.NodeID)
		if !ok {
			continue
		}
		q := qv.(*controlv2.Queue)
		sources, err := s.Repo.v2SwarmSources(ctx, task.NodeID, manifest)
		if err != nil {
			continue
		}
		body := protocolv2.SwarmSources{TaskID: task.TaskID, AttemptID: task.AttemptID, AssetID: assetID, ManifestID: manifest.ManifestID, Sources: sources}
		env, _ := protocolv2.New(protocolv2.TypeSwarmSources, mustID(), body)
		_ = q.Enqueue(env, assetID+"/"+task.AttemptID)
	}
}

func (r Repository) validateV2SwarmSourceRequest(ctx context.Context, nodeID string, req protocolv2.SwarmSourcesRequest) error {
	if req.TaskID == "" || req.AttemptID == "" || req.AssetID == "" || req.ManifestID == "" {
		return fmt.Errorf("swarm source request missing identity")
	}
	var one int
	err := r.DB.QueryRowContext(ctx, `SELECT 1 FROM node_tasks
		WHERE id=? AND node_id=? AND asset_id=? AND attempt_id=? AND state IN ('sent','running')`,
		req.TaskID, nodeID, req.AssetID, req.AttemptID).Scan(&one)
	if err == sql.ErrNoRows {
		return fmt.Errorf("swarm source request does not match active task")
	}
	return err
}
