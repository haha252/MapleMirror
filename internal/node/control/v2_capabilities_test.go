package control

import (
	protocolv2 "mirror-server/internal/protocol/v2"
	"testing"
)

func TestV2CapabilitiesAdvertisePeerOnlyMode(t *testing.T) {
	regular := (&Client{}).v2Capabilities()
	if !hasCapability(regular, protocolv2.CapabilityPeerBootstrap) {
		t.Fatal("missing peer bootstrap support")
	}
	if hasCapability(regular, "sync.peer_only.v1") {
		t.Fatal("regular node must remain origin-capable")
	}
	peerOnly := (&Client{ForcePeerDownload: true}).v2Capabilities()
	if !hasCapability(peerOnly, "sync.peer_only.v1") {
		t.Fatal("force-peer node must advertise sync.peer_only.v1")
	}
}

func hasCapability(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
