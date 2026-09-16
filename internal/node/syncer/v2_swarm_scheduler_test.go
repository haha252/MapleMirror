package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func schedulerManifest(assetID string, content []byte) protocolv2.SwarmManifest {
	pieceSize, pieceCount, _ := swarm.ChoosePieceSize(int64(len(content)))
	hashes := make([]byte, 0, pieceCount*32)
	for i := 0; i < pieceCount; i++ {
		start, end, _ := swarm.PieceBounds(int64(len(content)), pieceSize, i)
		sum := sha256.Sum256(content[start:end])
		hashes = append(hashes, sum[:]...)
	}
	whole := sha256.Sum256(content)
	m := protocolv2.SwarmManifest{AssetID: assetID, AssetSize: int64(len(content)),
		AssetSHA256: "sha256:" + hex.EncodeToString(whole[:]), PieceLayoutVersion: swarm.LayoutVersion,
		PieceSize: pieceSize, PieceCount: pieceCount, PieceHashAlgorithm: "sha256", PieceHashes: hashes}
	m.ManifestID = swarm.ManifestID(m.AssetID, m.AssetSHA256, m.AssetSize, m.PieceSize, m.PieceHashes)
	return m
}

func schedulerTask(m protocolv2.SwarmManifest, origin string) protocolv2.SyncTask {
	return protocolv2.SyncTask{TaskID: "task-" + m.AssetID, AttemptID: "attempt-1", TaskType: "asset_download",
		Asset: protocolv2.SyncAsset{AssetID: m.AssetID, ProjectID: "p1", Version: "v1", FileName: m.AssetID + ".bin",
			SizeBytes: m.AssetSize, DownloadURL: origin, DigestSHA256: m.AssetSHA256}, Manifest: &m}
}

func TestV2SwarmFallsBackToWholeOriginWhenOriginIgnoresRange(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-no-range", content)
	var hits atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(content) // Deliberately ignores Range and returns 200.
	}))
	defer origin.Close()
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: origin.Client(),
		AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), schedulerTask(manifest, origin.URL))
	if result.Result != "succeeded" {
		t.Fatalf("whole-origin fallback failed: %+v", result)
	}
	if hits.Load() < 2 {
		t.Fatalf("expected range attempt plus whole fallback, hits=%d", hits.Load())
	}
}

func TestV2SwarmOriginFallbackIndependentlyCrossChecksManifest(t *testing.T) {
	content := []byte("abcdef")
	bad := schedulerManifest("asset-bad-manifest", content)
	bad.PieceHashes[0] ^= 0xff
	bad.ManifestID = swarm.ManifestID(bad.AssetID, bad.AssetSHA256, bad.AssetSize, bad.PieceSize, bad.PieceHashes)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content) // no Range support forces whole-origin fallback
	}))
	defer origin.Close()
	db, storageDir, tempDir := prepareSyncer(t)
	result, crossCheck := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: origin.Client(),
		AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), schedulerTask(bad, origin.URL))
	if result.Result != "succeeded" || crossCheck == nil {
		t.Fatalf("fallback result=%+v crossCheck=%+v", result, crossCheck)
	}
	if crossCheck.ManifestID == bad.ManifestID {
		t.Fatal("whole-origin fallback trusted the incoming bad piece manifest")
	}
	want := schedulerManifest(bad.AssetID, content)
	if crossCheck.ManifestID != want.ManifestID {
		t.Fatalf("cross-check manifest=%s want=%s", crossCheck.ManifestID, want.ManifestID)
	}
}

func TestV2SwarmCompletesFromPeerWhenOriginUnavailable(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-peer-only", content)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "origin down", http.StatusBadGateway)
	}))
	defer origin.Close()
	peer := rangeServer(t, content)
	defer peer.Close()
	bits := make([]byte, swarm.BitsetBytes(manifest.PieceCount))
	swarm.Set(bits, 0)
	task := schedulerTask(manifest, origin.URL)
	task.Sources = []protocolv2.SwarmSource{{NodeID: "peer-1", BaseURL: peer.URL, Availability: bits}}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(),
		AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("peer-only swarm failed: %+v", result)
	}
}

func TestV2SwarmUsesDifferentPeersForDifferentPieces(t *testing.T) {
	content := make([]byte, 2*swarm.MinPieceSize)
	for i := range content {
		content[i] = byte((i * 31) % 251)
	}
	manifest := schedulerManifest("asset-multi-peer", content)
	if manifest.PieceCount != 2 {
		t.Fatalf("piece_count=%d", manifest.PieceCount)
	}
	var peer1Hits, peer2Hits atomic.Int64
	peer1 := rangeServerWithCounter(t, content, &peer1Hits)
	defer peer1.Close()
	peer2 := rangeServerWithCounter(t, content, &peer2Hits)
	defer peer2.Close()
	bits1 := make([]byte, swarm.BitsetBytes(2))
	bits2 := make([]byte, swarm.BitsetBytes(2))
	swarm.Set(bits1, 0)
	swarm.Set(bits2, 1)
	task := schedulerTask(manifest, "")
	task.Sources = []protocolv2.SwarmSource{
		{NodeID: "peer-1", BaseURL: peer1.URL, Availability: bits1},
		{NodeID: "peer-2", BaseURL: peer2.URL, Availability: bits2},
	}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer1.Client(),
		AllowPrivateSourceURLs: true, PeerFallbackWorkers: 2}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("multi-peer swarm failed: %+v", result)
	}
	if peer1Hits.Load() == 0 || peer2Hits.Load() == 0 {
		t.Fatalf("both peers must contribute: peer1=%d peer2=%d", peer1Hits.Load(), peer2Hits.Load())
	}
}

func TestV2SwarmSpreadsCompletePiecesAcrossPeers(t *testing.T) {
	content := make([]byte, 2*swarm.MinPieceSize)
	for i := range content {
		content[i] = byte((i * 17) % 251)
	}
	manifest := schedulerManifest("asset-complete-multi-peer", content)
	var peer1Hits, peer2Hits atomic.Int64
	peer1 := rangeServerWithCounter(t, content, &peer1Hits)
	defer peer1.Close()
	peer2 := rangeServerWithCounter(t, content, &peer2Hits)
	defer peer2.Close()
	bits := make([]byte, swarm.BitsetBytes(manifest.PieceCount))
	for i := 0; i < manifest.PieceCount; i++ {
		swarm.Set(bits, i)
	}
	task := schedulerTask(manifest, "")
	task.Sources = []protocolv2.SwarmSource{
		{NodeID: "peer-1", BaseURL: peer1.URL, Availability: bits, Complete: true},
		{NodeID: "peer-2", BaseURL: peer2.URL, Availability: bits, Complete: true},
	}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer1.Client(),
		AllowPrivateSourceURLs: true, PeerFallbackWorkers: 2}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("complete multi-peer swarm failed: %+v", result)
	}
	if peer1Hits.Load() == 0 || peer2Hits.Load() == 0 {
		t.Fatalf("complete peers should share piece load: peer1=%d peer2=%d", peer1Hits.Load(), peer2Hits.Load())
	}
}

func TestV2WholeModeUsesVerifiedPeerFallback(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-whole-peer", content)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer whole-token" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(content)
	}))
	defer peer.Close()
	task := schedulerTask(manifest, "https://origin.invalid/file")
	task.Manifest = nil
	task.SwarmDisabled = true
	task.WholeSources = []protocolv2.WholeSource{{NodeID: "peer", DownloadURL: peer.URL, Token: "whole-token"}}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(),
		AllowPrivateSourceURLs: true, ForcePeerDownload: true}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("whole peer fallback failed: %+v", result)
	}
}

func rangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return rangeServerWithCounter(t, content, nil)
}

func rangeServerWithCounter(t *testing.T, content []byte, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || end < start || end >= int64(len(content)) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[start : end+1])
	}))
}
