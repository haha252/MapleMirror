package control

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrCertificateNotActive = errors.New("证书未批准或已失效")
	ErrNodeDisabled         = errors.New("节点已禁用")
	ErrSessionUnavailable   = errors.New("控制会话不可用")
)

type Session struct {
	ID               string
	NodeID           string
	CertificateID    string
	RequestID        string
	AcceptedSequence uint64
}

func (r Repository) StartSession(ctx context.Context, certFingerprint, requestID string) (Session, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	var session Session
	var nodeState string
	err = tx.QueryRowContext(ctx, `SELECT c.id, c.node_id, n.state
		FROM node_certificates c JOIN nodes n ON n.id = c.node_id
		WHERE c.fingerprint = ? AND c.status = 'active' AND c.revoked_at IS NULL
		AND c.not_before <= ? AND c.not_after > ?`,
		certFingerprint, time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano)).Scan(&session.CertificateID, &session.NodeID, &nodeState)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, ErrCertificateNotActive
		}
		return Session{}, err
	}
	if nodeState == "disabled" {
		return Session{}, ErrNodeDisabled
	}
	session.ID, err = newID()
	if err != nil {
		return Session{}, err
	}
	session.RequestID = requestID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '新会话替换' WHERE node_id = ? AND disconnected_at IS NULL`, now, session.NodeID)
	if err := resetInterruptedTasks(ctx, tx, session.NodeID, now); err != nil {
		return Session{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_control_sessions
		(id, node_id, certificate_id, request_id, connected_at, last_message_sequence)
		VALUES (?, ?, ?, ?, ?, 0)`,
		session.ID, session.NodeID, session.CertificateID, requestID, now)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'syncing',
		routing_ready = 0, updated_at = ? WHERE id = ?`, now, session.NodeID)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	r.runtime().StartSession(session)
	return session, nil
}

func resetInterruptedTasks(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = '控制会话重连后重新派发', retry_after = NULL, updated_at = ?
		WHERE node_id = ? AND state IN ('sent', 'running')`, now, nodeID)
	return err
}

func (r Repository) CloseSession(ctx context.Context, sessionID, reason string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = ? WHERE id = ? AND disconnected_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), reason, sessionID)
	r.runtime().CloseSession(sessionID)
	return err
}
