package control

import (
	"encoding/hex"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func decodeHashAt(m protocolv2.SwarmManifest, index int) (string, bool) {
	if index < 0 || index >= m.PieceCount {
		return "", false
	}
	b := m.PieceHashes[index*32 : (index+1)*32]
	return "sha256:" + hex.EncodeToString(b), true
}
