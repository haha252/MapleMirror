package swarmstate

import (
	"sync"
	"testing"
)

func TestConcurrentPieceUpdatesAndPartialSnapshots(t *testing.T) {
	r := New()
	r.SetPartial(Partial{AssetID: "a", ManifestID: "m", Bitset: make([]byte, 8), Trusted: make([]byte, 8)})
	var wg sync.WaitGroup
	for piece := 0; piece < 64; piece++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.MarkPiece("a", "m", index)
				p, _ := r.Partial("a", "m")
				p.Bitset[0] = 0 // Snapshots must never mutate shared state.
			}
		}(piece)
	}
	wg.Wait()
	p, _ := r.Partial("a", "m")
	for i, b := range p.Bitset {
		if b != 255 || p.Trusted[i] != 255 {
			t.Fatalf("lost verified bits: %+v", p)
		}
	}
}
