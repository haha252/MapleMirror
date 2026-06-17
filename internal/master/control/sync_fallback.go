package control

import (
	"context"
	"net/url"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/downloadurl"
	"mirror-server/internal/protocol"
)

const maxSyncFallbackSources = 3
const maxReplicationTokenTTL = 2 * time.Minute
const defaultPeerFallbackWorkers = 8
const defaultPeerFallbackMinSizeBytes = 32 * 1024 * 1024

func (r Repository) syncFallbackSources(ctx context.Context, targetNodeID string, task protocol.SyncTask) []protocol.SyncFallbackSource {
	rows, err := r.DB.QueryContext(ctx, `SELECT n.id, n.public_name, n.public_download_base_url
		FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
		JOIN assets a ON a.id = ni.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		WHERE ni.asset_id = ? AND ni.node_id != ? AND n.state != 'disabled'
		AND n.state != 'offline' AND n.last_heartbeat_at IS NOT NULL
		AND n.last_heartbeat_at != '' AND n.public_download_base_url != '' AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		AND a.service_state IN ('candidate', 'pending', 'active') AND r.selected = 1 AND p.enabled = 1
		AND `+r.syncPeerPublicProbeSQL()+`
		ORDER BY n.public_name, n.id LIMIT ?`,
		r.syncPeerArgs(task.Asset.AssetID, targetNodeID, maxSyncFallbackSources)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	expires := time.Now().UTC().Add(r.replicationTokenTTL()).Format(time.RFC3339Nano)
	out := make([]protocol.SyncFallbackSource, 0, maxSyncFallbackSources)
	for rows.Next() {
		var nodeID, nodeName, baseURL string
		if err := rows.Scan(&nodeID, &nodeName, &baseURL); err != nil {
			return out
		}
		source := protocol.SyncFallbackSource{NodeID: nodeID, NodeName: nodeName}
		token, err := r.replicationToken(task, nodeID, targetNodeID, expires, 0, 0)
		if err != nil {
			continue
		}
		downloadURL, err := joinReplicationURL(baseURL, task.Asset.AssetID)
		if err != nil {
			continue
		}
		source.DownloadURL = downloadURL
		source.Token = token
		source.Parts = r.replicationParts(task, nodeID, targetNodeID, expires)
		out = append(out, source)
	}
	return out
}

func (r Repository) syncPeerPublicProbeSQL() string {
	if r.PublicProbeNetworkFailures <= 0 {
		return "1 = 1"
	}
	return "n.public_probe_network_failures < ?"
}

func (r Repository) syncPeerArgs(args ...any) []any {
	if r.PublicProbeNetworkFailures <= 0 {
		return args
	}
	out := make([]any, 0, len(args)+1)
	out = append(out, args[:2]...)
	out = append(out, r.PublicProbeNetworkFailures)
	out = append(out, args[2:]...)
	return out
}

func (r Repository) replicationToken(task protocol.SyncTask, sourceNodeID, targetNodeID, expires string,
	start, end int64) (string, error) {
	return r.ReplicationSigner.SignReplication(downloadtoken.ReplicationClaims{
		AssetID: task.Asset.AssetID, SourceNodeID: sourceNodeID, TargetNodeID: targetNodeID,
		ExpiresAt: expires, RequestID: task.TaskID, TaskID: task.TaskID,
		RangeStart: start, RangeEnd: end,
	})
}

func (r Repository) replicationParts(task protocol.SyncTask, sourceNodeID, targetNodeID, expires string) []protocol.SyncFallbackPart {
	size := task.Asset.SizeBytes
	if size < defaultPeerFallbackMinSizeBytes || size <= 0 {
		return nil
	}
	workers := defaultPeerFallbackWorkers
	if int64(workers) > size {
		workers = int(size)
	}
	partSize := (size + int64(workers) - 1) / int64(workers)
	parts := make([]protocol.SyncFallbackPart, 0, workers)
	for start := int64(0); start < size; start += partSize {
		end := start + partSize - 1
		if end >= size {
			end = size - 1
		}
		token, err := r.replicationToken(task, sourceNodeID, targetNodeID, expires, start, end)
		if err != nil {
			return nil
		}
		parts = append(parts, protocol.SyncFallbackPart{
			RangeStart: start, RangeEnd: end, Token: token,
		})
	}
	return parts
}

func (r Repository) replicationTokenTTL() time.Duration {
	if r.ReplicationTokenTTL > 0 && r.ReplicationTokenTTL < maxReplicationTokenTTL {
		return r.ReplicationTokenTTL
	}
	return maxReplicationTokenTTL
}

func joinReplicationURL(baseURL, assetID string) (string, error) {
	return downloadurl.Join(baseURL, "/internal/replication/"+url.PathEscape(assetID))
}
