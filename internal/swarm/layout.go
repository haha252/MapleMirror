package swarm

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

const (
	MinPieceSize         int64 = 1 << 20
	AutoMaxPieceSize     int64 = 64 << 20
	ProtocolMaxPieceSize int64 = 128 << 20
	TargetPieces         int64 = 1024
	MaxPieces            int64 = 8192
	BlockSize            int64 = 2 << 20
	LayoutVersion              = 1
)

var ErrAssetTooLarge = errors.New("asset exceeds swarm piece layout limit")

func ChoosePieceSize(size int64) (int64, int, error) {
	if size <= 0 {
		return MinPieceSize, 1, nil
	}
	piece := MinPieceSize
	for piece < AutoMaxPieceSize && ceilDiv(size, piece) > TargetPieces {
		piece <<= 1
	}
	for piece < ProtocolMaxPieceSize && ceilDiv(size, piece) > MaxPieces {
		piece <<= 1
	}
	count := ceilDiv(size, piece)
	if count > MaxPieces {
		return 0, 0, ErrAssetTooLarge
	}
	return piece, int(count), nil
}

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }

func PieceBounds(size, pieceSize int64, index int) (start, end int64, ok bool) {
	if size < 0 || pieceSize <= 0 || index < 0 {
		return 0, 0, false
	}
	start = int64(index) * pieceSize
	if start >= size {
		return 0, 0, false
	}
	end = start + pieceSize
	if end > size {
		end = size
	}
	return start, end, true // end exclusive
}

func BitsetBytes(pieceCount int) int {
	if pieceCount <= 0 {
		return 0
	}
	return (pieceCount + 7) / 8
}
func Has(bitset []byte, index int) bool {
	return index >= 0 && index/8 < len(bitset) && (bitset[index/8]&(1<<uint(index%8))) != 0
}
func Set(bitset []byte, index int) {
	if index >= 0 && index/8 < len(bitset) {
		bitset[index/8] |= 1 << uint(index%8)
	}
}
func Clear(bitset []byte, index int) {
	if index >= 0 && index/8 < len(bitset) {
		bitset[index/8] &^= 1 << uint(index%8)
	}
}
func Complete(bitset []byte, count int) bool {
	for i := 0; i < count; i++ {
		if !Has(bitset, i) {
			return false
		}
	}
	return true
}
func Count(bitset []byte, count int) int {
	n := 0
	for i := 0; i < count; i++ {
		if Has(bitset, i) {
			n++
		}
	}
	return n
}

func ManifestID(assetID, wholeDigest string, size, pieceSize int64, pieceHashes []byte) string {
	h := sha256.New()
	h.Write([]byte(assetID))
	h.Write([]byte{0})
	h.Write([]byte(wholeDigest))
	h.Write([]byte{0})
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(size))
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], uint64(pieceSize))
	h.Write(buf[:])
	h.Write(pieceHashes)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
