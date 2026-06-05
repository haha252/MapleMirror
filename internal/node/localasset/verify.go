package localasset

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	StateVerified = "verified"
	StateMissing  = "missing"
	StateMismatch = "mismatch"
)

type Record struct {
	RelativePath string
	DigestSHA256 string
	SizeBytes    int64
}

func CleanRelativePath(rel string) (string, bool) {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", false
	}
	return clean, true
}

func Verify(storage string, record Record) string {
	clean, ok := CleanRelativePath(record.RelativePath)
	if !ok {
		return StateMismatch
	}
	path := filepath.Join(storage, clean)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return StateMissing
	}
	digest, size, err := HashFile(path)
	if err != nil {
		return StateMissing
	}
	if digest != record.DigestSHA256 || size != record.SizeBytes {
		return StateMismatch
	}
	return StateVerified
}

func HashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", size, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}
