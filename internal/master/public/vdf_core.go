package public

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
)

const (
	vdfAlgorithm = "rsa-repeated-squaring-v1"
	vdfEncoding  = "base64url-uint-be-384"
	vdfByteSize  = 384
)

func encodeVDFInteger(value *big.Int) (string, error) {
	if value == nil || value.Sign() < 0 || value.BitLen() > vdfByteSize*8 {
		return "", errors.New("VDF 整数超出 384 字节范围")
	}
	buf := make([]byte, vdfByteSize)
	value.FillBytes(buf)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func decodeVDFInteger(encoded string) (*big.Int, []byte, error) {
	if len(encoded) != 512 {
		return nil, nil, errors.New("VDF 整数编码长度必须为 512")
	}
	buf, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(buf) != vdfByteSize || base64.RawURLEncoding.EncodeToString(buf) != encoded {
		return nil, nil, errors.New("VDF 整数必须为规范的无填充 base64url")
	}
	return new(big.Int).SetBytes(buf), buf, nil
}

func vdfModulusID(modulus *big.Int) (string, error) {
	encoded, err := encodeVDFInteger(modulus)
	if err != nil {
		return "", err
	}
	bytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func sampleVDFBase(random io.Reader, modulus *big.Int) (*big.Int, error) {
	if random == nil {
		random = rand.Reader
	}
	limit := new(big.Int).Sub(modulus, big.NewInt(3))
	if limit.Sign() <= 0 {
		return nil, errors.New("VDF 模数无效")
	}
	for attempts := 0; attempts < 128; attempts++ {
		candidate, err := rand.Int(random, limit)
		if err != nil {
			return nil, err
		}
		candidate.Add(candidate, big.NewInt(2))
		if new(big.Int).GCD(nil, nil, candidate, modulus).Cmp(big.NewInt(1)) == 0 {
			return candidate, nil
		}
	}
	return nil, errors.New("无法生成 VDF base")
}

func fastVDFSolution(base, modulus, lambda *big.Int, iterations uint64) *big.Int {
	exponent := new(big.Int).Exp(big.NewInt(2), new(big.Int).SetUint64(iterations), lambda)
	return new(big.Int).Exp(base, exponent, modulus)
}

func sequentialVDFSolution(base, modulus *big.Int, iterations uint64) *big.Int {
	result := new(big.Int).Set(base)
	for index := uint64(0); index < iterations; index++ {
		result.Mul(result, result).Mod(result, modulus)
	}
	return result
}

func vdfSolutionDigest(value *big.Int) ([32]byte, error) {
	var empty [32]byte
	encoded, err := encodeVDFInteger(value)
	if err != nil {
		return empty, err
	}
	buf, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return empty, err
	}
	return sha256.Sum256(buf), nil
}

func validVDFSolution(challenge Challenge, encoded string) bool {
	value, buf, err := decodeVDFInteger(encoded)
	if err != nil || challenge.Modulus == nil || value.Sign() <= 0 || value.Cmp(challenge.Modulus) >= 0 {
		return false
	}
	digest := sha256.Sum256(buf)
	return subtle.ConstantTimeCompare(digest[:], challenge.SolutionDigest[:]) == 1
}
