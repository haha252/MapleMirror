package probe

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

type Store struct {
	mu     sync.Mutex
	nodeID string
	key    *ecdsa.PrivateKey
	items  map[string]protocol.PublicProbeResponse
}

func NewStore(nodeID, keyFile string) (*Store, error) {
	key, err := readPrivateKey(keyFile)
	if err != nil {
		return nil, err
	}
	return NewStoreFromKey(nodeID, key), nil
}

func NewStoreFromPEM(nodeID string, keyPEM []byte) (*Store, error) {
	key, err := parsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return NewStoreFromKey(nodeID, key), nil
}

func NewStoreFromKey(nodeID string, key *ecdsa.PrivateKey) *Store {
	return &Store{nodeID: nodeID, key: key,
		items: map[string]protocol.PublicProbeResponse{}}
}

func (s *Store) Accept(challenge protocol.PublicProbeChallenge) error {
	if s == nil || challenge.ChallengeID == "" || challenge.Nonce == "" {
		return nil
	}
	if challenge.Algorithm != publicprobe.Algorithm ||
		!time.Now().UTC().Before(challenge.ExpiresAt) {
		return nil
	}
	signature, err := publicprobe.Sign(s.key, s.nodeID, challenge.ChallengeID,
		challenge.Nonce, challenge.ExpiresAt)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now().UTC())
	s.items[challenge.ChallengeID] = protocol.PublicProbeResponse{
		NodeID: s.nodeID, ChallengeID: challenge.ChallengeID,
		Nonce: challenge.Nonce, ExpiresAt: challenge.ExpiresAt,
		Signature: signature,
	}
	return nil
}

func (s *Store) Response(challengeID string) (protocol.PublicProbeResponse, bool) {
	if s == nil || challengeID == "" || strings.Contains(challengeID, "/") {
		return protocol.PublicProbeResponse{}, false
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	item, ok := s.items[challengeID]
	if !ok || !now.Before(item.ExpiresAt) {
		return protocol.PublicProbeResponse{}, false
	}
	return item, true
}

func (s *Store) cleanupLocked(now time.Time) {
	for id, item := range s.items {
		if !now.Before(item.ExpiresAt) {
			delete(s.items, id)
		}
	}
}

func readPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePrivateKey(data)
}

func parsePrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("node public probe private key PEM is invalid")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("node public probe private key is not ECDSA")
	}
	return key, nil
}
