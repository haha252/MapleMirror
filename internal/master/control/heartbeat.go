package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"mirror-server/internal/downloadurl"
	"mirror-server/internal/master/assignment"
	"mirror-server/internal/protocol"
)

type HeartbeatResult struct {
	AcceptedSequence uint64
	ManagedState     string
	RoutingReady     bool
	PublicProbe      *protocol.PublicProbeChallenge
}

func (r Repository) AcceptHeartbeat(ctx context.Context, session Session, seq uint64, hb protocol.Heartbeat) (HeartbeatResult, error) {
	downloadBaseURL := normalizedPublicDownloadBaseURL(hb.PublicDownloadBaseURL)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("控制会话不可用")
	}
	if seq <= last {
		ready := routingReady(ctx, tx, session.NodeID)
		return HeartbeatResult{AcceptedSequence: last, ManagedState: managedState(ready), RoutingReady: ready}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var previousMax int
	if err := tx.QueryRowContext(ctx, `SELECT max_mirror_projects FROM nodes
		WHERE id = ?`, session.NodeID).Scan(&previousMax); err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'online',
		last_heartbeat_at = ?, public_download_base_url = ?,
		target_bandwidth_bps = CASE WHEN ? > 0 THEN ? ELSE target_bandwidth_bps END,
		max_mirror_projects = ?,
		updated_at = ? WHERE id = ?`,
		now, downloadBaseURL, hb.Pressure.TargetBandwidthBPS,
		hb.Pressure.TargetBandwidthBPS, nonNegative(hb.MaxMirrorProjects),
		now, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if previousMax != nonNegative(hb.MaxMirrorProjects) {
		if err := assignment.ReconcileNode(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
		if _, err := assignment.GenerateNodeTasks(ctx, tx, session.NodeID, now); err != nil {
			return HeartbeatResult{}, err
		}
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	ready := r.nodeRoutingReady(ctx, session.NodeID)
	r.runtime().MarkHeartbeat(session.NodeID, runtimeHeartbeat{
		State: hb.Status, PressureRatio: hb.Pressure.Ratio,
		ActiveDownloads: int64(hb.ActiveDownloads), FreeBytes: hb.FreeBytes,
		TargetBandwidth: hb.Pressure.TargetBandwidthBPS,
		ActualBandwidth: hb.Pressure.ActualBandwidthBPS,
		ReportedAt:      now, Valid: true,
	})
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: managedState(ready), RoutingReady: ready}, nil
}

type OfflineSweepResult struct {
	OfflineNodes       int64
	ActiveDelayedNodes int64
}

func normalizedPublicDownloadBaseURL(value string) string {
	out, _ := downloadurl.NormalizeBase(value)
	return out
}

func (r Repository) MarkOffline(ctx context.Context, timeout time.Duration) (int64, error) {
	result, err := r.SweepOffline(ctx, timeout)
	return result.OfflineNodes, err
}

func (r Repository) SweepOffline(ctx context.Context, timeout time.Duration) (OfflineSweepResult, error) {
	cutoff := time.Now().UTC().Add(-timeout).Format(time.RFC3339Nano)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return OfflineSweepResult{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM nodes WHERE state NOT IN ('disabled', 'offline')
		AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?)`, cutoff)
	if err != nil {
		return OfflineSweepResult{}, err
	}
	defer rows.Close()
	var ids []string
	var active int64
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return OfflineSweepResult{}, err
		}
		if r.runtime().ActiveSession(id) {
			active++
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return OfflineSweepResult{}, err
	}
	if err := rows.Close(); err != nil {
		return OfflineSweepResult{}, err
	}
	if len(ids) == 0 {
		return OfflineSweepResult{ActiveDelayedNodes: active}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'offline',
		routing_ready = 0, updated_at = ? WHERE id IN (`+placeholders(len(ids))+`)`,
		append([]any{now}, stringArgs(ids)...)...)
	if err != nil {
		return OfflineSweepResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '心跳超时' WHERE node_id IN (`+placeholders(len(ids))+`) AND disconnected_at IS NULL`,
		append([]any{now}, stringArgs(ids)...)...)
	if err != nil {
		return OfflineSweepResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OfflineSweepResult{}, err
	}
	for _, id := range ids {
		r.runtime().CloseNodeSessions(id)
	}
	return OfflineSweepResult{OfflineNodes: int64(len(ids)), ActiveDelayedNodes: active}, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ",?"
	}
	return out
}

func stringArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, v := range values {
		args = append(args, v)
	}
	return args
}

func HeartbeatAck(result HeartbeatResult) json.RawMessage {
	body, _ := json.Marshal(protocol.HeartbeatAckPayload{
		AcceptedSequence: result.AcceptedSequence,
		ServerTime:       time.Now().UTC(),
		ManagedState:     result.ManagedState,
		RoutingReady:     result.RoutingReady,
		PublicProbe:      result.PublicProbe,
	})
	return body
}

func (r Repository) nodeRoutingReady(ctx context.Context, nodeID string) bool {
	return routingReady(ctx, r.DB, nodeID)
}

func routingReady(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID string) bool {
	var ready int
	_ = q.QueryRowContext(ctx, `SELECT routing_ready FROM nodes WHERE id = ?`, nodeID).Scan(&ready)
	return ready == 1
}

func managedState(ready bool) string {
	if ready {
		return "ready"
	}
	return "syncing"
}

func mustID() string {
	id, err := newID()
	if err != nil {
		return "id-error"
	}
	return id
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func finish(tx interface{ Commit() error }, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit()
}
