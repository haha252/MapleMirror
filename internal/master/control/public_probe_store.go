package control

import (
	"context"
	"crypto/x509"
	"database/sql"
	"time"
)

func (r Repository) PublicProbeBaseURL(ctx context.Context, nodeID string) (string, error) {
	var baseURL, state string
	err := r.DB.QueryRowContext(ctx, `SELECT COALESCE(public_download_base_url, ''),
		state FROM nodes WHERE id = ?`, nodeID).Scan(&baseURL, &state)
	if err != nil || state == "disabled" || state == "offline" {
		return "", err
	}
	return baseURL, nil
}

func (r Repository) ActiveNodeCertificate(ctx context.Context,
	nodeID string) (*x509.Certificate, error) {
	var certPEM string
	err := r.DB.QueryRowContext(ctx, `SELECT certificate_pem
		FROM node_certificates WHERE node_id = ? AND status = 'active'
		AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1`, nodeID).Scan(&certPEM)
	if err != nil {
		return nil, err
	}
	return parseCert([]byte(certPEM))
}

func (r Repository) RecordPublicProbeSuccess(ctx context.Context, nodeID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET last_public_probe_at = ?,
		last_public_probe_result = 'success', last_public_probe_error = '',
		public_probe_network_failures = 0, updated_at = ? WHERE id = ?`,
		now, now, nodeID)
	return err
}

func (r Repository) RecordPublicProbeNetworkFailure(ctx context.Context,
	nodeID string, threshold int, message string) (bool, error) {
	if threshold <= 0 {
		threshold = 1
	}
	failures, err := r.updatePublicProbeFailure(ctx, nodeID, "network_error",
		message, true)
	if err != nil || failures < threshold {
		return false, err
	}
	return true, r.markPublicProbeOffline(ctx, nodeID, "public probe network failure")
}

func (r Repository) RecordPublicProbeAnswerFailure(ctx context.Context,
	nodeID, message string) error {
	if _, err := r.updatePublicProbeFailure(ctx, nodeID, "answer_error",
		message, false); err != nil {
		return err
	}
	return r.markPublicProbeOffline(ctx, nodeID, "public probe answer failure")
}

func (r Repository) updatePublicProbeFailure(ctx context.Context, nodeID, result,
	message string, increment bool) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	expr := "public_probe_network_failures"
	if increment {
		expr = "public_probe_network_failures + 1"
	}
	_, err := r.DB.ExecContext(ctx, `UPDATE nodes SET last_public_probe_at = ?,
		last_public_probe_result = ?, last_public_probe_error = ?,
		public_probe_network_failures = `+expr+`, updated_at = ? WHERE id = ?`,
		now, result, truncateProbeError(message), now, nodeID)
	if err != nil {
		return 0, err
	}
	var failures int
	err = r.DB.QueryRowContext(ctx, `SELECT public_probe_network_failures
		FROM nodes WHERE id = ?`, nodeID).Scan(&failures)
	return failures, err
}

func (r Repository) markPublicProbeOffline(ctx context.Context, nodeID, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE nodes SET state = 'offline',
		routing_ready = 0, updated_at = ? WHERE id = ? AND state != 'disabled'`,
		now, nodeID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = ? WHERE node_id = ? AND disconnected_at IS NULL`,
		now, reason, nodeID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.runtime().CloseNodeSessions(nodeID)
	return nil
}

func truncateProbeError(message string) string {
	if len(message) <= 500 {
		return message
	}
	return message[:500]
}
