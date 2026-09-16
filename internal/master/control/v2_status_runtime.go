package control

import (
	"context"
	"errors"
	"time"

	"mirror-server/internal/downloadurl"
	protocolv2 "mirror-server/internal/protocol/v2"
)

var ErrInvalidV2Status = errors.New("control.v2 node status contains invalid values")

const v2StatusPersistInterval = 30 * time.Second

type runtimeV2Status struct {
	Status     protocolv2.NodeStatus
	ReportedAt time.Time
}

func (s *RuntimeStore) MarkV2Status(nodeID string, status runtimeV2Status) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	node := s.latest[nodeID]
	node.V2Status = status
	s.latest[nodeID] = node
}

func (s *RuntimeStore) LatestV2Status(nodeID string) (runtimeV2Status, bool) {
	if s == nil {
		return runtimeV2Status{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.latest[nodeID].V2Status
	return item, !item.ReportedAt.IsZero()
}

func (r Repository) persistV2LastSeenThrottled(ctx context.Context, nodeID string, status protocolv2.NodeStatus, now time.Time) error {
	runtime := r.runtime()
	runtime.mu.Lock()
	last := runtime.v2Persisted[nodeID]
	if !last.IsZero() && now.Sub(last) < v2StatusPersistInterval {
		runtime.mu.Unlock()
		return nil
	}
	runtime.v2Persisted[nodeID] = now
	runtime.mu.Unlock()
	baseURL := status.PublicDownloadBaseURL
	if normalized, ok := downloadurl.NormalizeBase(baseURL); ok {
		baseURL = normalized
	}
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET
		state = CASE WHEN state = 'disabled' THEN state ELSE 'online' END,
		last_heartbeat_at = ?, public_download_base_url = ?,
		target_bandwidth_bps = CASE WHEN ? > 0 THEN ? ELSE target_bandwidth_bps END,
		max_mirror_projects = ?, updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339Nano), baseURL, status.TargetBandwidthBPS, status.TargetBandwidthBPS,
		nonNegative(status.MaxMirrorProjects), now.Format(time.RFC3339Nano), nodeID)
	if err != nil {
		runtime.mu.Lock()
		delete(runtime.v2Persisted, nodeID)
		runtime.mu.Unlock()
	}
	return err
}
