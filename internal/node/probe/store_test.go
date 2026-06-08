package probe

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

func TestStoreSignsAcceptedChallenge(t *testing.T) {
	keyPath, key := writeTestKey(t)
	store, err := NewStore("node-1", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(time.Minute).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	if err := store.Accept(challenge); err != nil {
		t.Fatal(err)
	}
	response, ok := store.Response("challenge-1")
	if !ok {
		t.Fatal("expected public probe response")
	}
	if !publicprobe.Verify(&key.PublicKey, response.Signature,
		response.NodeID, response.ChallengeID, response.Nonce, response.ExpiresAt) {
		t.Fatal("public probe signature should verify")
	}
}

func TestStoreRejectsExpiredChallenge(t *testing.T) {
	keyPath, _ := writeTestKey(t)
	store, err := NewStore("node-1", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Accept(protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(-time.Minute).UTC(),
		Algorithm: publicprobe.Algorithm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Response("challenge-1"); ok {
		t.Fatal("expired public probe should not be stored")
	}
}

func writeTestKey(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "node.key")
	data := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, key
}
