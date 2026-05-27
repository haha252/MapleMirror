package control

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

type HeartbeatResult struct {
	AcceptedSequence uint64
	ManagedState     string
}

func (r Repository) AcceptHeartbeat(ctx context.Context, session Session, seq uint64, hb protocol.Heartbeat) (HeartbeatResult, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatResult{}, err
	}
	defer tx.Rollback()
	var last uint64
	err = tx.QueryRowContext(ctx, `SELECT last_message_sequence FROM node_control_sessions
		WHERE id = ? AND disconnected_at IS NULL`, session.ID).Scan(&last)
	if err != nil {
		return HeartbeatResult{}, fmt.Errorf("控制会话不可用")
	}
	if seq <= last {
		return HeartbeatResult{AcceptedSequence: last, ManagedState: "syncing"}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET
		last_message_sequence = ?, last_heartbeat_at = ? WHERE id = ?`, seq, now, session.ID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		last_heartbeat_at = ?, routing_ready = 0, updated_at = ? WHERE id = ?`,
		now, now, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_heartbeats
		(id, node_id, state, pressure_ratio, active_downloads, free_bytes, reported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		mustID(), session.NodeID, hb.Status, hb.Pressure.Ratio,
		hb.ActiveDownloads, hb.FreeBytes, now)
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, finish(tx, err)
}

func (r Repository) MarkOffline(ctx context.Context, timeout time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-timeout).Format(time.RFC3339Nano)
	result, err := r.DB.ExecContext(ctx, `UPDATE nodes SET state = 'offline',
		routing_ready = 0, updated_at = ? WHERE state != 'disabled'
		AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func HeartbeatAck(result HeartbeatResult) json.RawMessage {
	body, _ := json.Marshal(map[string]any{
		"accepted_sequence": result.AcceptedSequence,
		"server_time":       time.Now().UTC(),
		"managed_state":     result.ManagedState,
		"routing_ready":     false,
	})
	return body
}

func mustID() string {
	id, err := newID()
	if err != nil {
		return "id-error"
	}
	return id
}

func finish(tx interface{ Commit() error }, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit()
}
