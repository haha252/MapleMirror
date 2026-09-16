package v2

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
)

// StableMessageID derives a compact transport message ID from a domain identity.
// Length-prefixing each component avoids delimiter ambiguities while keeping IDs
// stable across reconnect/replay of the same logical item.
func StableMessageID(messageType string, parts ...string) string {
	h := sha256.New()
	writePart := func(value string) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(value))
	}
	writePart(Version)
	writePart(messageType)
	for _, part := range parts {
		writePart(part)
	}
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
