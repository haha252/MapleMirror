package control

import (
	"context"
	"time"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func (r Repository) AcceptV2NodeStatus(ctx context.Context, session Session, status protocolv2.NodeStatus) error {
	if status.SyncTaskSlotsAvailable < 0 || status.PublicActiveDownloads < 0 || status.SwarmActiveUploads < 0 || !status.DownloadPressure.Valid() {
		return ErrInvalidV2Status
	}
	now := time.Now().UTC()
	previous, hadPrevious := r.runtime().LatestV2Status(session.NodeID)
	r.runtime().MarkV2Status(session.NodeID, runtimeV2Status{Status: status, ReportedAt: now})
	r.runtime().SetSyncTaskSlotsAvailable(session.NodeID, status.SyncTaskSlotsAvailable)
	if err := r.refreshV2TaskLeases(ctx, session.NodeID, status.ActiveTasks); err != nil {
		return err
	}
	if v2StatusShouldWake(previous.Status, status, hadPrevious) {
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
	return capacityIncreased(previous.AssetFS, current.AssetFS) ||
		capacityIncreased(previous.PartialFS, current.PartialFS)
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
