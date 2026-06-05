package control

import (
	"context"
	"crypto/x509"
	"database/sql"
	"fmt"
	"time"

	"mirror-server/internal/controltls"
)

type SignedCertificate struct {
	NodeID         string
	CertificateID  string
	SerialNumber   string
	Fingerprint    string
	NotBefore      time.Time
	NotAfter       time.Time
	CertificatePEM string
	CAChainPEM     string
}

func (r Repository) PendingEnrollments(ctx context.Context) ([]Enrollment, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id, public_name, public_key_fingerprint,
		status, expires_at, request_id, capabilities_json FROM node_enrollment_requests
		WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Enrollment
	for rows.Next() {
		var item Enrollment
		var expires string
		if err := rows.Scan(&item.ID, &item.PublicName, &item.Fingerprint,
			&item.Status, &expires, &item.RequestID, &item.Capabilities); err != nil {
			return nil, err
		}
		item.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r Repository) CSRForEnrollment(ctx context.Context, id string) (string, Enrollment, error) {
	var csr, expires string
	var item Enrollment
	err := r.DB.QueryRowContext(ctx, `SELECT csr_pem, public_name,
		public_key_fingerprint, status, expires_at, request_id, capabilities_json
		FROM node_enrollment_requests WHERE id = ?`, id).Scan(&csr, &item.PublicName,
		&item.Fingerprint, &item.Status, &expires, &item.RequestID, &item.Capabilities)
	if err != nil {
		return "", Enrollment{}, err
	}
	item.ID = id
	item.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	return csr, item, nil
}

func (r Repository) CSRForNode(ctx context.Context, nodeID string) (string, string, error) {
	var csr, name string
	err := r.DB.QueryRowContext(ctx, `SELECT csr_pem, public_name
		FROM node_enrollment_requests WHERE node_id = ?
		ORDER BY created_at DESC LIMIT 1`, nodeID).Scan(&csr, &name)
	return csr, name, err
}

func (r Repository) RejectEnrollment(ctx context.Context, id, requestID, reason, admin string) error {
	result, err := r.DB.ExecContext(ctx, `UPDATE node_enrollment_requests
		SET status = 'rejected', rejected_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("登记请求状态不允许拒绝")
	}
	return r.Audit(ctx, "pairing.reject", "enrollment", id, "success", requestID, reason, admin)
}

func (r Repository) ApproveEnrollment(ctx context.Context, id, name, fp, requestID string, signed SignedCertificate) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentName, currentFP, status, expires string
	err = tx.QueryRowContext(ctx, `SELECT public_name, public_key_fingerprint,
		status, expires_at FROM node_enrollment_requests WHERE id = ?`, id).
		Scan(&currentName, &currentFP, &status, &expires)
	if err != nil {
		return err
	}
	expiresAt, _ := time.Parse(time.RFC3339Nano, expires)
	if status != "pending" || !time.Now().UTC().Before(expiresAt) {
		return fmt.Errorf("登记请求状态不允许审批")
	}
	if currentName != name || currentFP != fp {
		return fmt.Errorf("管理员确认的节点材料不一致")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	replacedNodeIDs, err := deleteQuarantinedNodesByName(ctx, tx, name, signed.NodeID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		routing_ready, created_at, updated_at) VALUES (?, ?, ?, 'offline', 0, 0, ?, ?)`,
		signed.NodeID, name, signed.Fingerprint, now, now)
	if err != nil {
		return fmt.Errorf("创建节点身份失败：%w", err)
	}
	if err := seedNodeTargets(ctx, tx, signed.NodeID, now); err != nil {
		return err
	}
	if err := insertCertificate(ctx, tx, signed, requestID, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_enrollment_requests SET status = 'approved',
		approved_at = ?, node_id = ?, certificate_id = ? WHERE id = ?`,
		now, signed.NodeID, signed.CertificateID, id)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, nodeID := range replacedNodeIDs {
		r.runtime().CloseNodeSessions(nodeID)
	}
	return nil
}

func insertCertificate(ctx context.Context, tx *sql.Tx, cert SignedCertificate, requestID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after, status,
		issued_request_id, created_at, certificate_pem, ca_chain_pem)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)`,
		cert.CertificateID, cert.NodeID, cert.SerialNumber, cert.Fingerprint,
		cert.NotBefore.Format(time.RFC3339Nano), cert.NotAfter.Format(time.RFC3339Nano),
		requestID, now, cert.CertificatePEM, cert.CAChainPEM)
	return err
}

func (r Repository) RotateCertificate(ctx context.Context, nodeID, requestID, admin string, signed SignedCertificate) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `UPDATE node_certificates SET status = 'revoked',
		revoked_at = ? WHERE node_id = ? AND status = 'active'`, now, nodeID)
	if err != nil {
		return err
	}
	if err := insertCertificate(ctx, tx, signed, requestID, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET certificate_fingerprint = ?,
		state = 'offline', routing_ready = 0, updated_at = ? WHERE id = ?`, signed.Fingerprint, now, nodeID)
	if err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '证书轮换' WHERE node_id = ? AND disconnected_at IS NULL`, now, nodeID)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_audit_events
		(id, operation, target_type, target_id, result, request_id, created_at,
		admin_identity, details_summary) VALUES (?, 'certificate.rotate', 'node', ?,
		'success', ?, ?, ?, '节点证书已轮换')`,
		mustID(), nodeID, requestID, now, admin)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.runtime().CloseNodeSessions(nodeID)
	return nil
}

func CertRecord(nodeID, certID string, cert *x509.Certificate, pemText, caChain string) SignedCertificate {
	return SignedCertificate{NodeID: nodeID, CertificateID: certID,
		SerialNumber: cert.SerialNumber.String(), Fingerprint: controltls.Fingerprint(cert),
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter,
		CertificatePEM: pemText, CAChainPEM: caChain}
}
