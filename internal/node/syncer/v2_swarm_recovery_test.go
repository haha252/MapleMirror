package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mirror-server/internal/node/swarmstate"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func TestV2SwarmWaitsForPeerCooldownAndRecoversWithoutOrigin(t *testing.T) {
	content := []byte("recover from a temporary peer failure")
	m := schedulerManifest("recover-peer", content)
	good := rangeServer(t, content)
	defer good.Close()
	var hits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		good.Config.Handler.ServeHTTP(w, r)
	}))
	defer peer.Close()
	task := schedulerTask(m, "https://origin.invalid/unreachable")
	task.Sources = []protocolv2.SwarmSource{{NodeID: "peer", BaseURL: peer.URL, Availability: []byte{1}}}
	db, storageDir, tempDir := prepareSyncer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, _ := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(),
		Swarm: swarmstate.New(), AllowPrivateSourceURLs: true, ForcePeerDownload: true}).ExecuteV2(ctx, task)
	if result.Result != "succeeded" || hits.Load() < 2 {
		t.Fatalf("peer did not recover: %+v hits=%d", result, hits.Load())
	}
}

func TestV2SwarmFailedPieceDoesNotDiscardOtherPieceProgress(t *testing.T) {
	content := make([]byte, 2*swarm.MinPieceSize)
	m := schedulerManifest("recover-progress", content)
	good := rangeServer(t, content)
	defer good.Close()
	goodStarted, badFinished := make(chan struct{}), make(chan struct{})
	var startOnce, finishOnce sync.Once
	var goodHits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var start, end int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		if start >= m.PieceSize {
			goodHits.Add(1)
			startOnce.Do(func() { close(goodStarted) })
			select {
			case <-badFinished:
			case <-r.Context().Done():
				return
			}
			timer := time.NewTimer(50 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
			good.Config.Handler.ServeHTTP(w, r)
			return
		}
		select {
		case <-goodStarted:
		case <-r.Context().Done():
			return
		}
		body := append([]byte(nil), content[start:end+1]...)
		body[0] = 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
		if end+1 == m.PieceSize {
			finishOnce.Do(func() { close(badFinished) })
		}
	}))
	defer peer.Close()
	task := schedulerTask(m, "")
	task.Sources = []protocolv2.SwarmSource{{NodeID: "peer", BaseURL: peer.URL, Availability: []byte{3}}}
	db, storageDir, tempDir := prepareSyncer(t)
	e := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(), Swarm: swarmstate.New(),
		AllowPrivateSourceURLs: true, ForcePeerDownload: true, PeerFallbackWorkers: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, _ := e.ExecuteV2(ctx, task)
	if result.Result == "succeeded" {
		t.Fatal("corrupt piece unexpectedly succeeded")
	}
	var bits []byte
	if err := db.QueryRow(`SELECT verified_bitmap FROM swarm_partials WHERE asset_id=?`, m.AssetID).Scan(&bits); err != nil {
		t.Fatal(err)
	}
	if swarm.Has(bits, 0) || !swarm.Has(bits, 1) {
		t.Fatalf("healthy piece progress was lost: %v result=%+v", bits, result)
	}
	task.AttemptID = "attempt-2"
	task.Sources[0].BaseURL = good.URL
	result, _ = e.ExecuteV2(ctx, task)
	if result.Result != "succeeded" {
		t.Fatalf("resume failed: %+v", result)
	}
}
