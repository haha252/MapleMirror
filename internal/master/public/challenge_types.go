package public

import (
	"io"
	"math/big"
	"sync"
)

type Challenge struct {
	ID              string
	SourceKind      string
	ProtocolVersion string
	Algorithm       string
	AssetID         string
	AssetSizeBytes  int64
	ClientPrefixKey string
	CreatedAt       string
	Nonce           string
	Difficulty      int
	ModulusID       string
	Modulus         *big.Int
	ModulusEncoded  string
	BaseEncoded     string
	Iterations      uint64
	Multiplier      int
	SolutionDigest  [32]byte
	ExpiresAt       string
}

type vdfService struct {
	keys      *vdfKeyManager
	policyMu  sync.RWMutex
	policy    vdfPolicy
	semaphore chan struct{}
	random    io.Reader
}
