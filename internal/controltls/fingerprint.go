package controltls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"
)

func Fingerprint(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func NormalizeFingerprint(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "")
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
