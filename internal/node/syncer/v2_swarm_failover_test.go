package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func TestV2SwarmSwitchesPeerAfterShortBlock(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-peer-switch", content)
	var badHits, goodHits atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badHits.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(content)-1, len(content)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[:len(content)/2])
	}))
	defer bad.Close()
	good := rangeServerWithCounter(t, content, &goodHits)
	defer good.Close()
	bits := make([]byte, swarm.BitsetBytes(manifest.PieceCount))
	swarm.Set(bits, 0)
	task := schedulerTask(manifest, "")
	task.Sources = []protocolv2.SwarmSource{
		{NodeID: "bad", BaseURL: bad.URL, Availability: bits},
		{NodeID: "good", BaseURL: good.URL, Availability: bits},
	}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: good.Client(),
		AllowPrivateSourceURLs: true, ForcePeerDownload: true}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("peer switch failed: %+v", result)
	}
	if badHits.Load() == 0 || goodHits.Load() == 0 {
		t.Fatalf("expected failed and replacement peer to be used: bad=%d good=%d", badHits.Load(), goodHits.Load())
	}
}

func TestV2SwarmRejectsPieceHashMismatch(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-piece-corrupt", content)
	corrupt := []byte("abcdeg")
	peer := rangeServer(t, corrupt)
	defer peer.Close()
	bits := make([]byte, swarm.BitsetBytes(manifest.PieceCount))
	swarm.Set(bits, 0)
	task := schedulerTask(manifest, "")
	task.Sources = []protocolv2.SwarmSource{{NodeID: "bad", BaseURL: peer.URL, Availability: bits}}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(),
		AllowPrivateSourceURLs: true, ForcePeerDownload: true}).ExecuteV2(context.Background(), task)
	if result.Result == "succeeded" {
		t.Fatalf("corrupted piece must not succeed: %+v", result)
	}
	var verified int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_assets WHERE asset_id=? AND state='verified'`, manifest.AssetID).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if verified != 0 {
		t.Fatal("corrupted piece became a verified local asset")
	}
}

func TestV2SwarmUsesOriginWhenAllPeersFail(t *testing.T) {
	content := []byte("abcdef")
	manifest := schedulerManifest("asset-origin-after-peers", content)
	var peerHits, originHits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerHits.Add(1)
		http.Error(w, "peer down", http.StatusBadGateway)
	}))
	defer peer.Close()
	origin := rangeServerWithCounter(t, content, &originHits)
	defer origin.Close()
	bits := make([]byte, swarm.BitsetBytes(manifest.PieceCount))
	swarm.Set(bits, 0)
	task := schedulerTask(manifest, origin.URL)
	task.Sources = []protocolv2.SwarmSource{{NodeID: "bad-peer", BaseURL: peer.URL, Availability: bits}}
	db, storageDir, tempDir := prepareSyncer(t)
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: origin.Client(),
		AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" {
		t.Fatalf("origin fallback after peer failure failed: %+v", result)
	}
	if peerHits.Load() == 0 || originHits.Load() == 0 {
		t.Fatalf("expected peer failure followed by origin: peer=%d origin=%d", peerHits.Load(), originHits.Load())
	}
}
