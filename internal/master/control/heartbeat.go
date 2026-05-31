package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"mirror-server/internal/protocol"
)

type HeartbeatResult struct {
	AcceptedSequence uint64
	ManagedState     string
}

func (r Repository) AcceptHeartbeat(ctx context.Context, session Session, seq uint64, hb protocol.Heartbeat) (HeartbeatResult, error) {
	if !validPublicDownloadBaseURL(hb.PublicDownloadBaseURL) {
		return HeartbeatResult{}, fmt.Errorf("节点公网下载地址不合法")
	}
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
		last_heartbeat_at = ?, public_download_base_url = ?, updated_at = ? WHERE id = ?`,
		now, hb.PublicDownloadBaseURL, now, session.NodeID)
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

func validPublicDownloadBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (r Repository) MarkOffline(ctx context.Context, timeout time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-timeout).Format(time.RFC3339Nano)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM nodes WHERE state NOT IN ('disabled', 'offline')
		AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'offline',
		routing_ready = 0, updated_at = ? WHERE id IN (`+placeholders(len(ids))+`)`,
		append([]any{now}, stringArgs(ids)...)...)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '心跳超时' WHERE node_id IN (`+placeholders(len(ids))+`) AND disconnected_at IS NULL`,
		append([]any{now}, stringArgs(ids)...)...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
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
	body, _ := json.Marshal(map[string]any{
		"accepted_sequence": result.AcceptedSequence,
		"server_time":       time.Now().UTC(),
		"managed_state":     result.ManagedState,
		"routing_ready":     result.ManagedState == "ready",
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
