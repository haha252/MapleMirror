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

func (r Repository) syncFallbackSources(ctx context.Context, targetNodeID string, task protocol.SyncTask) []protocol.SyncFallbackSource {
	rows, err := r.DB.QueryContext(ctx, `SELECT n.id, n.public_name, n.public_download_base_url
		FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
		JOIN assets a ON a.id = ni.asset_id
		WHERE ni.asset_id = ? AND ni.node_id != ? AND n.state != 'disabled'
		AND n.state != 'offline' AND n.last_heartbeat_at IS NOT NULL
		AND n.last_heartbeat_at != '' AND n.public_download_base_url != '' AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		ORDER BY n.public_name, n.id LIMIT ?`,
		task.Asset.AssetID, targetNodeID, maxSyncFallbackSources)
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
		token, err := r.ReplicationSigner.SignReplication(downloadtoken.ReplicationClaims{
			AssetID: task.Asset.AssetID, SourceNodeID: nodeID, TargetNodeID: targetNodeID,
			ExpiresAt: expires, RequestID: task.TaskID, TaskID: task.TaskID,
		})
		if err != nil {
			continue
		}
		downloadURL, err := joinReplicationURL(baseURL, task.Asset.AssetID)
		if err != nil {
			continue
		}
		out = append(out, protocol.SyncFallbackSource{
			NodeID: nodeID, NodeName: nodeName,
			DownloadURL: downloadURL,
			Token:       token,
		})
	}
	return out
}

func (r Repository) replicationTokenTTL() time.Duration {
	if r.ReplicationTokenTTL > 0 {
		return r.ReplicationTokenTTL
	}
	return 15 * time.Minute
}

func joinReplicationURL(baseURL, assetID string) (string, error) {
	return downloadurl.Join(baseURL, "/internal/replication/"+url.PathEscape(assetID))
}
