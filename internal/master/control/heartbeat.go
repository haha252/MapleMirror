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
		return HeartbeatResult{AcceptedSequence: last, ManagedState: "syncing"}, tx.Commit()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		last_heartbeat_at = ?, public_download_base_url = ?, updated_at = ? WHERE id = ?`,
		now, downloadBaseURL, now, session.NodeID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HeartbeatResult{}, err
	}
	r.runtime().MarkHeartbeat(session.NodeID, runtimeHeartbeat{
		State: hb.Status, PressureRatio: hb.Pressure.Ratio,
		ActiveDownloads: int64(hb.ActiveDownloads), FreeBytes: hb.FreeBytes,
		ReportedAt: now, Valid: true,
	})
	return HeartbeatResult{AcceptedSequence: seq, ManagedState: "syncing"}, nil
}

func normalizedPublicDownloadBaseURL(value string) string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return value
	}
	return ""
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
	for _, id := range ids {
		r.runtime().CloseNodeSessions(id)
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
