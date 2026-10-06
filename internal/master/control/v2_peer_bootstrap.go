package control

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

// Keep the tentative claim in the same transaction as source selection. Never
// query Repository.DB here: SQLite deployments use a single connection.
func (r Repository) preparePeerBootstrap(ctx context.Context, tx *sql.Tx, nodeID string, task *protocolv2.SyncTask) (bool, error) {
	if task.TaskType != "asset_download" || task.Asset.SizeBytes > swarm.ProtocolMaxPieceSize*swarm.MaxPieces {
		return true, nil
	}
	var present int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM asset_piece_manifests WHERE asset_id=?)`, task.Asset.AssetID).Scan(&present); err != nil {
		return false, err
	}
	if present != 0 {
		return true, nil
	}
	if !r.runtime().PeerBootstrap(nodeID) {
		return !r.runtime().PeerOnly(nodeID), nil
	}
	rows, err := tx.QueryContext(ctx, r.syncFallbackSelectSQL(), r.syncFallbackArgs(task.Asset.AssetID, nodeID, maxSyncFallbackCandidates)...)
	if err != nil {
		return false, err
	}
	type peer struct{ id, name, base string }
	var peers []peer
	for rows.Next() {
		var p peer
		if err := rows.Scan(&p.id, &p.name, &p.base); err != nil {
			rows.Close()
			return false, err
		}
		peers = append(peers, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	legacy := protocol.SyncTask{TaskID: task.TaskID, Asset: protocol.SyncAsset{AssetID: task.Asset.AssetID, SizeBytes: task.Asset.SizeBytes}}
	expires := time.Now().UTC().Add(r.replicationTokenTTL()).Format(time.RFC3339Nano)
	for _, p := range peers {
		url, err := joinReplicationURL(p.base, task.Asset.AssetID)
		if err != nil {
			continue
		}
		token, err := r.replicationToken(legacy, p.id, nodeID, expires, 0, 0)
		if err != nil {
			return false, fmt.Errorf("sign peer bootstrap authorization: %w", err)
		}
		source := protocolv2.WholeSource{NodeID: p.id, NodeName: p.name, DownloadURL: url, Token: token}
		for _, part := range r.replicationParts(legacy, p.id, nodeID, expires) {
			source.Parts = append(source.Parts, protocolv2.WholePart{RangeStart: part.RangeStart, RangeEnd: part.RangeEnd, Token: part.Token})
		}
		task.WholeSources = append(task.WholeSources, source)
		if len(task.WholeSources) >= maxSyncFallbackSources {
			break
		}
	}
	return len(task.WholeSources) > 0 || !r.runtime().PeerOnly(nodeID), nil
}
