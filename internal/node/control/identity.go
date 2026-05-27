package control

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mirror-server/internal/controltls"
)

type IdentityStore struct {
	DB       *sql.DB
	CertFile string
	KeyFile  string
	CAFile   string
}

func (s IdentityStore) Save(nodeID string, certPEM, caPEM []byte) error {
	if err := writePrivate(s.CertFile, certPEM, 0o600); err != nil {
		return err
	}
	if len(caPEM) > 0 {
		if err := writePrivate(s.CAFile, caPEM, 0o644); err != nil {
			return err
		}
	}
	cert, err := parseCert(certPEM)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO control_identity
		(id, node_id, certificate_fingerprint, certificate_not_after, enrolled_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET node_id = excluded.node_id,
		certificate_fingerprint = excluded.certificate_fingerprint,
		certificate_not_after = excluded.certificate_not_after,
		enrolled_at = excluded.enrolled_at`,
		nodeID, controltls.Fingerprint(cert), cert.NotAfter.Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s IdentityStore) NodeID() (string, error) {
	var nodeID string
	err := s.DB.QueryRow(`SELECT node_id FROM control_identity WHERE id = 1`).Scan(&nodeID)
	return nodeID, err
}

func writePrivate(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return fmt.Errorf("身份材料路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseCert(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("节点证书格式无效")
	}
	return x509.ParseCertificate(block.Bytes)
}
