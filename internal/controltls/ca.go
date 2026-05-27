package controltls

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

func ParseCSR(pemText []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemText)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, fmt.Errorf("CSR 格式无效")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析 CSR 失败：%w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR 签名无效：%w", err)
	}
	return csr, nil
}

func SignNode(ca *x509.Certificate, caKey any, csr *x509.CertificateRequest, nodeID string, ttl time.Duration) ([]byte, *x509.Certificate, error) {
	if ca == nil || caKey == nil || csr == nil || nodeID == "" {
		return nil, nil, fmt.Errorf("签发节点证书缺少必要材料")
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("生成证书序列号失败：%w", err)
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: nodeID},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, csr.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("签发节点证书失败：%w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("解析已签发证书失败：%w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, nil
}
