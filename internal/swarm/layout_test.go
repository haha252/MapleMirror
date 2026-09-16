package swarm

import "testing"

func TestChoosePieceSize(t *testing.T) {
	cases := []struct {
		size, piece int64
		count       int
	}{{100 << 20, 1 << 20, 100}, {1 << 30, 1 << 20, 1024}, {2 << 30, 2 << 20, 1024}, {10 << 30, 16 << 20, 640}, {50 << 30, 64 << 20, 800}}
	for _, c := range cases {
		p, n, e := ChoosePieceSize(c.size)
		if e != nil || p != c.piece || n != c.count {
			t.Fatalf("size=%d p=%d n=%d err=%v", c.size, p, n, e)
		}
	}
}
func TestBitset(t *testing.T) {
	b := make([]byte, BitsetBytes(10))
	Set(b, 9)
	if !Has(b, 9) || Has(b, 8) {
		t.Fatal("bitset")
	}
	Clear(b, 9)
	if Has(b, 9) {
		t.Fatal("clear")
	}
}
