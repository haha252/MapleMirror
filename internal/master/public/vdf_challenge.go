package public

import (
	"context"
	"errors"
	"math/big"
	"time"

	"mirror-server/internal/requestid"
)

func (s *Store) CreateVDFChallenge(ctx context.Context, source, assetID, prefix string,
	level abuseLevel, ttl time.Duration) (Challenge, error) {
	if s.VDF == nil || s.VDF.keys == nil {
		return Challenge{}, errors.New("VDF 未初始化")
	}
	now := time.Now().UTC()
	challenges := s.challengeMemory()
	if err := challenges.reserve(prefix, now); err != nil {
		return Challenge{}, err
	}
	committed := false
	defer func() {
		if !committed {
			challenges.releaseReservation(prefix)
		}
	}()
	select {
	case s.VDF.semaphore <- struct{}{}:
		defer func() { <-s.VDF.semaphore }()
	default:
		return Challenge{}, errVDFBusy
	}
	id, err := requestid.New()
	if err != nil {
		return Challenge{}, err
	}
	sizeBytes, err := s.routableAsset(ctx, assetID)
	if err != nil {
		return Challenge{}, err
	}
	key := s.VDF.keys.current()
	iterations, multiplier := s.VDF.parameters(sizeBytes, level)
	base, solution, err := s.createVDFExpected(key, iterations)
	if err != nil {
		return Challenge{}, err
	}
	baseEncoded, err := encodeVDFInteger(base)
	if err != nil {
		return Challenge{}, err
	}
	digest, err := vdfSolutionDigest(solution)
	if err != nil {
		return Challenge{}, err
	}
	challenge := Challenge{ID: id, SourceKind: source, ProtocolVersion: "v2", Algorithm: vdfAlgorithm,
		AssetID: assetID, AssetSizeBytes: sizeBytes, ClientPrefixKey: prefix,
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(ttl).Format(time.RFC3339Nano),
		ModulusID: key.modulusID, Modulus: new(big.Int).Set(key.modulus), ModulusEncoded: key.encoded,
		BaseEncoded: baseEncoded, Iterations: iterations, Multiplier: multiplier, SolutionDigest: digest}
	challenges.put(challenge, now)
	committed = true
	return challenge, nil
}

func (s *Store) createVDFExpected(key *vdfKeyMaterial, iterations uint64) (*big.Int, *big.Int, error) {
	one := big.NewInt(1)
	minusOne := new(big.Int).Sub(key.modulus, one)
	for attempts := 0; attempts < 64; attempts++ {
		base, err := sampleVDFBase(s.VDF.random, key.modulus)
		if err != nil {
			return nil, nil, err
		}
		solution := fastVDFSolution(base, key.modulus, key.lambda, iterations)
		if solution.Cmp(one) != 0 && solution.Cmp(minusOne) != 0 {
			return base, solution, nil
		}
	}
	return nil, nil, errors.New("VDF 预期答案退化")
}
