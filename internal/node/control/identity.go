package control

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"mirror-server/internal/controltls"
	"mirror-server/internal/downloadtoken"
)

type IdentityStore struct {
	DB       *sql.DB
	CertFile string
	KeyFile  string
	CAFile   string
}

type IdentityMaterials struct {
	NodeID                    string
	CertificatePEM            []byte
	CAPEM                     []byte
	PrivateKeyPEM             []byte
	DownloadTokenPublicKeyPEM []byte
}

func (s IdentityStore) Save(nodeID string, certPEM, caPEM []byte) error {
	return s.SaveWithToken(nodeID, certPEM, caPEM, nil)
}

func (s IdentityStore) SaveWithToken(nodeID string, certPEM, caPEM, tokenPEM []byte) error {
	cert, err := parseCert(certPEM)
	if err != nil {
		return err
	}
	keyPEM, err := s.pendingPrivateKey()
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO control_identity
		(id, node_id, certificate_fingerprint, certificate_not_after, enrolled_at,
		certificate_pem, ca_pem, private_key_pem, download_token_public_key_pem)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET node_id = excluded.node_id,
		certificate_fingerprint = excluded.certificate_fingerprint,
		certificate_not_after = excluded.certificate_not_after,
		enrolled_at = excluded.enrolled_at,
		certificate_pem = excluded.certificate_pem,
		ca_pem = excluded.ca_pem,
		private_key_pem = excluded.private_key_pem,
		download_token_public_key_pem =
			COALESCE(NULLIF(excluded.download_token_public_key_pem, ''),
				control_identity.download_token_public_key_pem)`,
		nodeID, controltls.Fingerprint(cert), cert.NotAfter.Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano), string(certPEM), string(caPEM),
		string(keyPEM), string(tokenPEM))
	if err != nil {
		return err
	}
	return s.ClearEnrollmentState()
}

func (s IdentityStore) NodeID() (string, error) {
	materials, err := s.Materials()
	if err != nil {
		return "", err
	}
	return materials.NodeID, nil
}

func (s IdentityStore) Materials() (IdentityMaterials, error) {
	if imported, err := s.importLegacyIdentity(); err != nil {
		return IdentityMaterials{}, err
	} else if imported {
		return s.loadMaterials()
	}
	return s.loadMaterials()
}

func (s IdentityStore) TLSConfig(serverName string, withClientCert bool) (*tls.Config, error) {
	materials, err := s.Materials()
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM := []byte(nil), []byte(nil)
	if withClientCert {
		certPEM, keyPEM = materials.CertificatePEM, materials.PrivateKeyPEM
	}
	return controltls.NodeClientFromPEM(materials.CAPEM, certPEM, keyPEM, serverName)
}

func (s IdentityStore) DownloadTokenVerifier() (downloadtoken.Signer, error) {
	materials, err := s.Materials()
	if err != nil {
		return downloadtoken.Signer{}, err
	}
	return downloadtoken.NewVerifierFromPublicPEM(materials.DownloadTokenPublicKeyPEM)
}

func (s IdentityStore) SaveDownloadTokenPublicKey(data []byte) error {
	_, err := s.DB.Exec(`UPDATE control_identity
		SET download_token_public_key_pem = ? WHERE id = 1`, string(data))
	return err
}

func (s IdentityStore) PrivateKeyPEM() ([]byte, error) {
	materials, err := s.Materials()
	if err != nil {
		return nil, err
	}
	return materials.PrivateKeyPEM, nil
}

func (s IdentityStore) loadMaterials() (IdentityMaterials, error) {
	var out IdentityMaterials
	var certPEM, caPEM, keyPEM, tokenPEM string
	err := s.DB.QueryRow(`SELECT node_id, COALESCE(certificate_pem, ''),
		COALESCE(ca_pem, ''), COALESCE(private_key_pem, ''),
		COALESCE(download_token_public_key_pem, '')
		FROM control_identity WHERE id = 1`).
		Scan(&out.NodeID, &certPEM, &caPEM, &keyPEM, &tokenPEM)
	if err != nil {
		return IdentityMaterials{}, err
	}
	out.CertificatePEM = []byte(certPEM)
	out.CAPEM = []byte(caPEM)
	out.PrivateKeyPEM = []byte(keyPEM)
	out.DownloadTokenPublicKeyPEM = []byte(tokenPEM)
	if len(out.CertificatePEM) == 0 || len(out.CAPEM) == 0 || len(out.PrivateKeyPEM) == 0 {
		return IdentityMaterials{}, fmt.Errorf("节点身份材料不完整")
	}
	return out, nil
}

func (s IdentityStore) importLegacyIdentity() (bool, error) {
	var nodeID, certPEM, caPEM, keyPEM string
	err := s.DB.QueryRow(`SELECT node_id, COALESCE(certificate_pem, ''),
		COALESCE(ca_pem, ''), COALESCE(private_key_pem, '')
		FROM control_identity WHERE id = 1`).Scan(&nodeID, &certPEM, &caPEM, &keyPEM)
	if err != nil {
		return false, nil
	}
	if certPEM != "" && caPEM != "" && keyPEM != "" {
		return false, nil
	}
	certData, certErr := os.ReadFile(s.CertFile)
	caData, caErr := os.ReadFile(s.CAFile)
	keyData, keyErr := os.ReadFile(s.KeyFile)
	if certErr != nil || caErr != nil || keyErr != nil {
		return false, nil
	}
	cert, err := parseCert(certData)
	if err != nil {
		return false, err
	}
	_, err = s.DB.Exec(`UPDATE control_identity SET certificate_fingerprint = ?,
		certificate_not_after = ?, certificate_pem = ?, ca_pem = ?, private_key_pem = ?
		WHERE id = 1`,
		controltls.Fingerprint(cert), cert.NotAfter.Format(time.RFC3339Nano),
		string(certData), string(caData), string(keyData))
	return err == nil, err
}

func parseCert(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("节点证书格式无效")
	}
	return x509.ParseCertificate(block.Bytes)
}
