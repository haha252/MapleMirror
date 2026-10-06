package control

import (
	"context"
	"database/sql"
	"net/url"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/downloadurl"
	"mirror-server/internal/protocol"
)

const maxSyncFallbackSources = 3
const maxSyncFallbackCandidates = maxSyncFallbackSources * 3
const maxReplicationTokenTTL = 2 * time.Minute
const defaultPeerFallbackWorkers = 8
const defaultPeerFallbackMinSizeBytes = 32 * 1024 * 1024

func (r Repository) syncFallbackSources(ctx context.Context, targetNodeID string, task protocol.SyncTask) []protocol.SyncFallbackSource {
	rows, err := r.DB.QueryContext(ctx, r.syncFallbackSelectSQL(), r.syncFallbackArgs(task.Asset.AssetID, targetNodeID, maxSyncFallbackCandidates)...)
	if err != nil {
		return nil
	}
	return r.readSyncFallbackSources(ctx, rows, targetNodeID, task)
}

func (r Repository) hasUsablePeerFallback(ctx context.Context, tx *sql.Tx, targetNodeID string, task protocol.SyncTask) bool {
	rows, err := tx.QueryContext(ctx, r.syncFallbackExistsSQL(), r.syncFallbackArgs(task.Asset.AssetID, targetNodeID, 1)...)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var baseURL string
		if err := rows.Scan(&baseURL); err != nil {
			return false
		}
		if _, err := joinReplicationURL(baseURL, task.Asset.AssetID); err == nil {
			return true
		}
	}
	return false
}

func (r Repository) readSyncFallbackSources(ctx context.Context, rows *sql.Rows, targetNodeID string,
	task protocol.SyncTask) []protocol.SyncFallbackSource {
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
		if len(out) >= maxSyncFallbackSources {
			break
		}
	}
	return out
}

func (r Repository) syncFallbackSelectSQL() string {
	return `SELECT n.id, n.public_name, n.public_download_base_url
` + syncPeerInventorySQL("?", "?") + `
		ORDER BY
			CASE WHEN ? > 0 AND n.public_probe_network_failures >= ? THEN 1 ELSE 0 END,
			n.public_probe_network_failures,
			CASE WHEN n.last_public_probe_result = 'success' THEN 0 ELSE 1 END,
			COALESCE(n.last_public_probe_at, '') DESC,
			COALESCE(n.last_heartbeat_at, '') DESC,
			n.target_bandwidth_bps DESC,
			n.public_name, n.id
		LIMIT ?`
}

func (r Repository) syncFallbackExistsSQL() string {
	return `SELECT n.public_download_base_url
` + syncPeerInventorySQL("?", "?") + `
		ORDER BY
			CASE WHEN ? > 0 AND n.public_probe_network_failures >= ? THEN 1 ELSE 0 END,
			n.public_probe_network_failures,
			CASE WHEN n.last_public_probe_result = 'success' THEN 0 ELSE 1 END,
			COALESCE(n.last_public_probe_at, '') DESC,
			COALESCE(n.last_heartbeat_at, '') DESC,
			n.target_bandwidth_bps DESC,
			n.public_name, n.id
		LIMIT ?`
}

func (r Repository) syncFallbackArgs(assetID, targetNodeID string, limit int) []any {
	return []any{assetID, targetNodeID, r.PublicProbeNetworkFailures, r.PublicProbeNetworkFailures, limit}
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
