package controltls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
)

func Fingerprint(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func PublicKeyFingerprint(csr *x509.CertificateRequest) (string, error) {
	if csr == nil || csr.PublicKey == nil {
		return "", fmt.Errorf("CSR 公钥为空")
	}
	pubDER, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	if err != nil {
		return "", fmt.Errorf("CSR 公钥格式无效：%w", err)
	}
	sum := sha256.Sum256(pubDER)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func NormalizeFingerprint(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "")
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
