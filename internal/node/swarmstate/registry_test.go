package swarmstate

import (
	protocolv2 "mirror-server/internal/protocol/v2"
	"testing"
	"time"
)

func TestRegistryCopiesState(t *testing.T) {
	r := New()
	m := protocolv2.SwarmManifest{ManifestID: "m", PieceHashes: []byte{1}}
	r.SetManifest(m)
	m.PieceHashes[0] = 9
	got, _ := r.Manifest("m")
	if got.PieceHashes[0] != 1 {
		t.Fatal("manifest aliased")
	}
}

func TestSourceFailureBackoffAndRecovery(t *testing.T) {
	r := New()
	if !r.SourceReady("m", "n") {
		t.Fatal("new source should be ready")
	}
	r.ReportSourceFailure("m", "n")
	if r.SourceReady("m", "n") {
		t.Fatal("failed source should be in backoff")
	}
	r.ReportSourceSuccess("m", "n")
	if !r.SourceReady("m", "n") {
		t.Fatal("successful source should leave backoff")
	}
}

func TestAvailabilityWakeIsCoalesced(t *testing.T) {
	r := New()
	r.SetPartial(Partial{AssetID: "a", ManifestID: "m", Bitset: make([]byte, 1), Trusted: make([]byte, 1)})
	ch := make(chan struct{}, 4)
	r.SetWake(func() { ch <- struct{}{} })
	r.MarkPiece("a", "m", 0)
	r.MarkPiece("a", "m", 1)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("missing coalesced wake")
	}
	select {
	case <-ch:
		t.Fatal("piece updates should coalesce into one wake")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSourceRefreshAge(t *testing.T) {
	r := New()
	if !r.NeedsSourceRefresh("m", time.Minute) {
		t.Fatal("missing snapshot should need refresh")
	}
	r.SetSources("m", nil)
	if r.NeedsSourceRefresh("m", time.Minute) {
		t.Fatal("fresh empty snapshot should still be fresh")
	}
}
