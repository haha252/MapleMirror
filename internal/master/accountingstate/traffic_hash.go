package accountingstate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

func TrafficEventHash(nodeID string, event protocol.TrafficEvent) string {
	h := sha256.New()
	parts := []string{
		nodeID,
		fmt.Sprint(event.EventSequence),
		event.AuthorizationID,
		event.AssetID,
		event.NodeRequestID,
		event.MasterRequestID,
		fmt.Sprint(event.SentBytes),
		event.Status,
		event.ReportedAt.UTC().Format(time.RFC3339Nano),
	}
	for _, part := range parts {
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
