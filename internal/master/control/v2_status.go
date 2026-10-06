package control

import (
	"context"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) AcceptV2NodeStatus(ctx context.Context, session Session, status protocolv2.NodeStatus) error {
	if status.SyncTaskSlotsAvailable < 0 || status.PublicActiveDownloads < 0 || status.SwarmActiveUploads < 0 || !status.DownloadPressure.Valid() || !status.MirrorTraffic.Valid() {
		return ErrInvalidV2Status
	}
	unlock := r.runtime().lockV2Tasks(session.NodeID)
	defer unlock()
	if _, err := r.runtime().CurrentSequence(session); err != nil {
		return err
	}
	now := time.Now().UTC()
	previous, hadPrevious := r.runtime().LatestV2Status(session.NodeID)
	gate := r.runtime().v2DispatchState(session.NodeID)
	if (hadPrevious && previous.Status.CapacityRevision != 0 && status.CapacityRevision <= previous.Status.CapacityRevision) ||
		(gate.CapacityBarrier != 0 && status.CapacityRevision <= gate.CapacityBarrier) {
		return nil // A queued pre-rejection status must not reopen admission.
	}
	if err := r.refreshV2TaskLeases(ctx, session.NodeID, status.ActiveTasks); err != nil {
		return err
	}
	r.runtime().MarkV2Status(session.NodeID, runtimeV2Status{Status: status, ReportedAt: now})
	r.runtime().SetSyncTaskSlotsAvailable(session.NodeID, status.SyncTaskSlotsAvailable)
	recovered := gate.WaitingStatus && (status.CapacityRevision != 0 || !now.Before(gate.PauseUntil))
	if recovered {
		gate.WaitingStatus = false
		r.runtime().setV2DispatchState(session.NodeID, gate)
	}
	if recovered || v2StatusShouldWake(previous.Status, status, hadPrevious) {
		r.runtime().NotifySyncTasks(session.NodeID)
	}
	return r.persistV2LastSeenThrottled(ctx, session.NodeID, status, now)
}

func v2StatusShouldWake(previous, current protocolv2.NodeStatus, hadPrevious bool) bool {
	if !hadPrevious {
		return current.SyncTaskSlotsAvailable > 0
	}
	if current.SyncTaskSlotsAvailable > previous.SyncTaskSlotsAvailable {
		return true
	}
	if current.SyncTaskSlotsAvailable <= 0 {
		return false
	}
	if !sameV2ActiveTasks(previous.ActiveTasks, current.ActiveTasks) {
		return true
	}
	return capacityIncreased(previous.AssetFS, current.AssetFS) ||
		capacityIncreased(previous.PartialFS, current.PartialFS)
}

func sameV2ActiveTasks(previous, current []protocolv2.ActiveTask) bool {
	seen := make(map[protocolv2.ActiveTask]bool, len(previous))
	next := make(map[protocolv2.ActiveTask]bool, len(current))
	for _, task := range previous {
		seen[task] = true
	}
	for _, task := range current {
		if !seen[task] {
			return false
		}
		next[task] = true
	}
	return len(seen) == len(next)
}

func capacityIncreased(previous, current protocolv2.FilesystemCapacity) bool {
	if !current.Valid {
		return false
	}
	if !previous.Valid {
		return true
	}
	previousUsable := previous.AvailableBytes - previous.ReservedBytes
	currentUsable := current.AvailableBytes - current.ReservedBytes
	return currentUsable > previousUsable
}
