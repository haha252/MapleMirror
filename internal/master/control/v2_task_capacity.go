package control

import (
	"context"
	"math"
	protocolv2 "mirror-server/internal/protocol/v2"
	"time"
)

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

func (r Repository) v2DispatchAllowance(ctx context.Context, nodeID string) (int, error) {
	gate := r.runtime().v2DispatchState(nodeID)
	if gate.WaitingStatus || time.Now().Before(gate.PauseUntil) {
		return 0, nil
	}
	latest, known := r.runtime().LatestV2Status(nodeID)
	slots := latest.Status.SyncTaskSlotsAvailable
	if !known || slots <= 0 {
		return 0, nil
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id, COALESCE(attempt_id,'') FROM node_tasks WHERE node_id=?
		AND state IN ('sent','running') AND lease_expires_at>?`, nodeID,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	reflected := make(map[protocolv2.ActiveTask]bool)
	for _, task := range latest.Status.ActiveTasks {
		reflected[task] = true
	}
	unreflected := 0
	for rows.Next() {
		var task protocolv2.ActiveTask
		if err := rows.Scan(&task.TaskID, &task.AttemptID); err != nil {
			return 0, err
		}
		if !reflected[task] {
			unreflected++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if slots <= unreflected {
		return 0, nil
	}
	return slots - unreflected, nil
}
