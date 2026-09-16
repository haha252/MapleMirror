package files

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/node/swarmstate"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func swarmToken(t *testing.T, signer downloadtoken.Signer, asset, manifest, source string, size, pieceSize int64, pieces int) string {
	t.Helper()
	token, err := signer.SignSwarm(downloadtoken.SwarmClaims{
		AssetID: asset, ManifestID: manifest, SourceNodeID: source, TargetNodeID: "node-target",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Scope: "swarm_piece_read",
		AssetSize: size, PieceSize: pieceSize, PieceCount: pieces,
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestSwarmCommittedRange(t *testing.T) {
	db, storage, signer := prepareNodeFile(t)
	piece := swarm.MinPieceSize
	// prepareNodeFile has a six-byte verified asset. The protocol piece remains 1 MiB.
	token := swarmToken(t, signer, "asset-1", "manifest-1", "node-1", 6, piece, 1)
	req := httptest.NewRequest(http.MethodGet, "/internal/swarm/asset-1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=1-3")
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storage, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "bcd" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 1-3/6" {
		t.Fatalf("content-range=%q", got)
	}
}

func TestSwarmRejectsCapabilityAndRangeFailures(t *testing.T) {
	db, storage, signer := prepareNodeFile(t)
	piece := swarm.MinPieceSize
	cases := []struct {
		name, source, rng string
		expires           time.Time
		want              int
	}{
		{"wrong source", "other", "bytes=0-1", time.Now().Add(time.Minute), http.StatusUnauthorized},
		{"expired", "node-1", "bytes=0-1", time.Now().Add(-time.Minute), http.StatusUnauthorized},
		{"no range", "node-1", "", time.Now().Add(time.Minute), http.StatusRequestedRangeNotSatisfiable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := signer.SignSwarm(downloadtoken.SwarmClaims{AssetID: "asset-1", ManifestID: "manifest-1",
				SourceNodeID: tc.source, TargetNodeID: "node-target", ExpiresAt: tc.expires.UTC().Format(time.RFC3339Nano),
				Scope: "swarm_piece_read", AssetSize: 6, PieceSize: piece, PieceCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/internal/swarm/asset-1", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if tc.rng != "" {
				req.Header.Set("Range", tc.rng)
			}
			rec := httptest.NewRecorder()
			(&Handler{DB: db, Storage: storage, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("code=%d want=%d", rec.Code, tc.want)
			}
		})
	}
}

func TestSwarmPartialLazyRevalidationAndRevocation(t *testing.T) {
	db, storage, signer := prepareNodeFile(t)
	_, _ = db.Exec(`DELETE FROM local_assets WHERE asset_id='asset-1'`)
	registry := swarmstate.New()
	pieceSize := swarm.MinPieceSize
	content := make([]byte, pieceSize+3)
	copy(content[:6], []byte("abcdef"))
	path := filepath.Join(t.TempDir(), "partial.part")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	p0 := sha256.Sum256(content[:pieceSize])
	p1 := sha256.Sum256(content[pieceSize:])
	hashes := append(append([]byte{}, p0[:]...), p1[:]...)
	manifest := protocolv2.SwarmManifest{ManifestID: "manifest-part", AssetID: "asset-part", AssetSize: int64(len(content)),
		AssetSHA256: "sha256:unused", PieceLayoutVersion: swarm.LayoutVersion, PieceSize: pieceSize,
		PieceCount: 2, PieceHashAlgorithm: "sha256", PieceHashes: hashes}
	registry.SetManifest(manifest)
	bits := make([]byte, swarm.BitsetBytes(2))
	swarm.Set(bits, 0)
	registry.SetPartial(swarmstate.Partial{AssetID: manifest.AssetID, ManifestID: manifest.ManifestID, Path: path,
		Bitset: bits, Trusted: make([]byte, len(bits))})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO swarm_partials(asset_id,manifest_id,partial_path,asset_size,piece_size,piece_count,verified_bitmap,created_at,updated_at,last_access_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, manifest.AssetID, manifest.ManifestID, path, manifest.AssetSize, pieceSize, 2, bits, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	token := swarmToken(t, signer, manifest.AssetID, manifest.ManifestID, "node-1", manifest.AssetSize, pieceSize, 2)
	req := httptest.NewRequest(http.MethodGet, "/internal/swarm/asset-part", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-1")
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storage, NodeID: "node-1", Signer: signer, Swarm: registry}).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "ab" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	got, _ := registry.Partial(manifest.AssetID, manifest.ManifestID)
	if !swarm.Has(got.Trusted, 0) {
		t.Fatal("piece should be runtime-trusted after lazy hash")
	}

	// Corrupt the piece, clear runtime trust to simulate a restart, and verify the
	// persisted availability is revoked instead of serving bad bytes.
	content[0] = 'X'
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got.Trusted = make([]byte, len(got.Trusted))
	registry.SetPartial(got)
	req = httptest.NewRequest(http.MethodGet, "/internal/swarm/asset-part", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-1")
	rec = httptest.NewRecorder()
	(&Handler{DB: db, Storage: storage, NodeID: "node-1", Signer: signer, Swarm: registry}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("corrupt piece code=%d", rec.Code)
	}
	got, _ = registry.Partial(manifest.AssetID, manifest.ManifestID)
	if swarm.Has(got.Bitset, 0) {
		t.Fatal("corrupt piece availability was not revoked")
	}
	var persisted []byte
	if err := db.QueryRow(`SELECT verified_bitmap FROM swarm_partials WHERE asset_id=? AND manifest_id=?`, manifest.AssetID, manifest.ManifestID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if swarm.Has(persisted, 0) {
		t.Fatal("corrupt piece persisted bitmap was not revoked")
	}
}
