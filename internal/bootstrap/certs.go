package bootstrap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func EnsureCA(certPath, keyPath, name string) error {
	if exists(certPath) && exists(keyPath) {
		return nil
	}
	if exists(certPath) || exists(keyPath) {
		return fmt.Errorf("证书材料不完整：%s / %s", certPath, keyPath)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tpl, err := certTemplate(name, true)
	if err != nil {
		return err
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	return writeCertKey(certPath, keyPath, der, key)
}

func EnsureServerCert(certPath, keyPath, caCertPath, caKeyPath, name string, hosts []string) error {
	if exists(certPath) && exists(keyPath) {
		return nil
	}
	if exists(certPath) || exists(keyPath) {
		return fmt.Errorf("证书材料不完整：%s / %s", certPath, keyPath)
	}
	caCert, caKey, err := readCA(caCertPath, caKeyPath)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tpl, err := certTemplate(name, false)
	if err != nil {
		return err
	}
	tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if host != "" {
			tpl.DNSNames = append(tpl.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	return writeCertKey(certPath, keyPath, der, key)
}

func certTemplate(name string, isCA bool) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	keyUsage := x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	if isCA {
		keyUsage |= x509.KeyUsageCertSign
	}
	return &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name},
		NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(5, 0, 0),
		KeyUsage:              keyUsage,
		BasicConstraintsValid: true, IsCA: isCA,
	}, nil
}

func readCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	return cert, key, err
}

func writeCertKey(certPath, keyPath string, der []byte, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	return writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600)
}

func writePEM(path, typ string, der []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
