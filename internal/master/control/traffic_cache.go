package control

import (
	"context"
	"database/sql"

	"mirror-server/internal/master/statbuffer"
)

const trafficDedupeRowsPerNode = 10000

func pruneTrafficDedupeWindow(ctx context.Context, tx *sql.Tx, nodeID string, sequence uint64) error {
	if sequence <= trafficDedupeRowsPerNode {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM traffic_event_dedupe
		WHERE node_id = ? AND event_sequence < ?`,
		nodeID, int64(sequence-trafficDedupeRowsPerNode))
	return err
}

func (r Repository) bufferTrafficStats(info authAccounting, bytes int64) {
	counters := statbuffer.Counter{Started: info.StartedIncrement, Bytes: bytes}
	r.StatsBuffer.AddPublic(info.Day, counters)
	r.StatsBuffer.AddAsset(info.AssetID, counters)
	r.StatsBuffer.AddProject(info.ProjectID, counters)
	r.StatsBuffer.AddNode(info.NodeID, bytes)
}
