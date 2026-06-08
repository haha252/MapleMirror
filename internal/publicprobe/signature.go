package publicprobe

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"time"
)

const Algorithm = "ecdsa-p256-sha256"

func Message(nodeID, challengeID, nonce string, expiresAt time.Time) []byte {
	return []byte(fmt.Sprintf("mirror-node-public-probe.v1\n%s\n%s\n%s\n%s",
		nodeID, challengeID, nonce, expiresAt.UTC().Format(time.RFC3339Nano)))
}

func Sign(key *ecdsa.PrivateKey, nodeID, challengeID, nonce string,
	expiresAt time.Time) (string, error) {
	if key == nil {
		return "", fmt.Errorf("node public probe private key is missing")
	}
	sum := sha256.Sum256(Message(nodeID, challengeID, nonce, expiresAt))
	signature, err := ecdsa.SignASN1(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func Verify(publicKey any, signature, nodeID, challengeID, nonce string,
	expiresAt time.Time) bool {
	key, ok := publicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve == nil {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(Message(nodeID, challengeID, nonce, expiresAt))
	return ecdsa.VerifyASN1(key, sum[:], raw)
}

func PublicKeyFromCert(cert *x509.Certificate) any {
	if cert == nil {
		return nil
	}
	return cert.PublicKey
}
