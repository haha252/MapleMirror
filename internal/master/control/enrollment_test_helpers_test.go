package control

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"mirror-server/internal/controltls"
)

func testEnrollmentCSR(t *testing.T, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	csr, err := controltls.ParseCSR([]byte(csrPEM))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := controltls.PublicKeyFingerprint(csr)
	if err != nil {
		t.Fatal(err)
	}
	return csrPEM, fingerprint
}
