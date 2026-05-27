package control

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type CertificateDelivery struct {
	EnrollmentID   string
	NodeID         string
	CertificatePEM string
	CAChainPEM     string
	NotAfter       time.Time
}

func (r Repository) CollectCertificate(ctx context.Context, enrollmentID string) (CertificateDelivery, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return CertificateDelivery{}, err
	}
	defer tx.Rollback()
	var d CertificateDelivery
	var certID, notAfter string
	err = tx.QueryRowContext(ctx, `SELECT e.id, e.node_id, e.certificate_id,
		c.certificate_pem, c.ca_chain_pem, c.not_after FROM node_enrollment_requests e
		JOIN node_certificates c ON c.id = e.certificate_id
		WHERE e.id = ? AND e.status = 'approved' AND e.collected_at IS NULL`,
		enrollmentID).Scan(&d.EnrollmentID, &d.NodeID, &certID,
		&d.CertificatePEM, &d.CAChainPEM, &notAfter)
	if err != nil {
		if err == sql.ErrNoRows {
			return CertificateDelivery{}, fmt.Errorf("证书尚不可领取")
		}
		return CertificateDelivery{}, err
	}
	d.NotAfter, _ = time.Parse(time.RFC3339Nano, notAfter)
	_, err = tx.ExecContext(ctx, `UPDATE node_enrollment_requests
		SET status = 'collected', collected_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), enrollmentID)
	if err != nil {
		return CertificateDelivery{}, err
	}
	return d, tx.Commit()
}
