package control

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"mirror-server/internal/controltls"
)

type CertificateSigner struct {
	CA         *x509.Certificate
	CAKey      any
	CAChainPEM string
	TTL        time.Duration
}

func LoadCertificateSigner(certFile, keyFile string, ttl time.Duration) (CertificateSigner, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return CertificateSigner{}, fmt.Errorf("读取签发 CA 证书失败：%w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return CertificateSigner{}, fmt.Errorf("读取签发 CA 私钥失败：%w", err)
	}
	ca, err := parseCert(certPEM)
	if err != nil {
		return CertificateSigner{}, err
	}
	key, err := parseKey(keyPEM)
	if err != nil {
		return CertificateSigner{}, err
	}
	return CertificateSigner{CA: ca, CAKey: key, CAChainPEM: string(certPEM), TTL: ttl}, nil
}

func (s CertificateSigner) Sign(csr *x509.CertificateRequest) (SignedCertificate, error) {
	nodeID, err := newID()
	if err != nil {
		return SignedCertificate{}, err
	}
	certID, err := newID()
	if err != nil {
		return SignedCertificate{}, err
	}
	pemText, cert, err := controltls.SignNode(s.CA, s.CAKey, csr, nodeID, s.TTL)
	if err != nil {
		return SignedCertificate{}, err
	}
	return CertRecord(nodeID, certID, cert, string(pemText), s.CAChainPEM), nil
}

func parseCert(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("签发 CA 证书格式无效")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKey(data []byte) (any, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("签发 CA 私钥格式无效")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
