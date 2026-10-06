package syncer

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	protocolv2 "mirror-server/internal/protocol/v2"
)

func TestV2PeerBootstrapVerifiesAndBuildsManifestWithoutOrigin(t *testing.T) {
	content := []byte("abcdef")
	want := schedulerManifest("peer-bootstrap", content)
	var originHits, peerHits atomic.Int64
	origin := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { originHits.Add(1); http.Error(w, "forbidden", 500) }))
	defer origin.Close()
	peer := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerHits.Add(1)
		if r.Header.Get("Authorization") != "Bearer replication-token" {
			t.Error("missing replication authorization")
		}
		_, _ = w.Write(content)
	}))
	defer peer.Close()
	db, storageDir, tempDir := prepareSyncer(t)
	task := schedulerTask(want, origin.URL)
	task.Manifest = nil
	task.Bootstrap = true
	task.WholeSources = []protocolv2.WholeSource{{NodeID: "legacy-donor", DownloadURL: peer.URL, Token: "replication-token"}}
	executor := Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: peer.Client(), ForcePeerDownload: true, AllowPrivateSourceURLs: true}
	result, manifest := executor.ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" || manifest == nil || manifest.ManifestID != want.ManifestID {
		t.Fatalf("result=%+v manifest=%+v", result, manifest)
	}
	// Re-execution after lost ACK uses the verified file and regenerates the same manifest.
	result, manifest = executor.ExecuteV2(context.Background(), task)
	if result.Result != "succeeded" || manifest == nil || manifest.ManifestID != want.ManifestID || peerHits.Load() != 1 || originHits.Load() != 0 {
		t.Fatalf("reused result=%+v manifest=%+v peer=%d origin=%d", result, manifest, peerHits.Load(), originHits.Load())
	}
}

func TestV2PeerBootstrapSkipsBadAndExpiredPeers(t *testing.T) {
	for _, failure := range []string{"digest", "size", "expired"} {
		t.Run(failure, func(t *testing.T) {
			content := []byte("abcdef")
			want := schedulerManifest("bootstrap-failover", content)
			bad := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch failure {
				case "expired":
					http.Error(w, "expired", http.StatusUnauthorized)
				case "size":
					_, _ = w.Write([]byte("abc"))
				default:
					_, _ = w.Write([]byte("xxxxxx"))
				}
			}))
			defer bad.Close()
			good := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(content) }))
			defer good.Close()
			db, storageDir, tempDir := prepareSyncer(t)
			task := schedulerTask(want, "https://origin.invalid/never")
			task.Manifest = nil
			task.Bootstrap = true
			task.WholeSources = []protocolv2.WholeSource{{NodeID: "bad", DownloadURL: bad.URL}, {NodeID: "good", DownloadURL: good.URL}}
			result, manifest := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: good.Client(), ForcePeerDownload: true, AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), task)
			if result.Result != "succeeded" || manifest == nil || manifest.ManifestID != want.ManifestID {
				t.Fatalf("result=%+v manifest=%+v", result, manifest)
			}
		})
	}
}

func TestV2PeerBootstrapFailureNeverCommitsManifestOrUsesOrigin(t *testing.T) {
	var hits atomic.Int64
	origin := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = w.Write([]byte("abcdef")) }))
	defer origin.Close()
	bad := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("xxxxxx")) }))
	defer bad.Close()
	db, storageDir, tempDir := prepareSyncer(t)
	task := schedulerTask(schedulerManifest("bootstrap-bad", []byte("abcdef")), origin.URL)
	task.Manifest = nil
	task.Bootstrap = true
	task.WholeSources = []protocolv2.WholeSource{{NodeID: "bad", DownloadURL: bad.URL}}
	result, manifest := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: bad.Client(), ForcePeerDownload: true, AllowPrivateSourceURLs: true}).ExecuteV2(context.Background(), task)
	if result.Result != "digest_mismatch" || manifest != nil || hits.Load() != 0 {
		t.Fatalf("result=%+v manifest=%+v origin=%d", result, manifest, hits.Load())
	}
}

func TestV2PeerBootstrapOriginFallbackAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			var hits atomic.Int64
			content := []byte("abcdef")
			want := schedulerManifest("bootstrap-origin-fallback", content)
			origin := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = w.Write(content) }))
			defer origin.Close()
			bad := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
			defer bad.Close()
			db, storageDir, tempDir := prepareSyncer(t)
			task := schedulerTask(want, origin.URL)
			task.Manifest = nil
			task.Bootstrap = true
			task.WholeSources = []protocolv2.WholeSource{{NodeID: "bad", DownloadURL: bad.URL}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			result, manifest := (Executor{DB: db, Storage: storageDir, TempDir: tempDir, Client: origin.Client(), AllowPrivateSourceURLs: true}).ExecuteV2(ctx, task)
			if cancelled {
				if result.Result == "succeeded" || manifest != nil || hits.Load() != 0 {
					t.Fatalf("cancelled result=%+v manifest=%+v hits=%d", result, manifest, hits.Load())
				}
				return
			}
			if result.Result != "succeeded" || manifest == nil || manifest.ManifestID != want.ManifestID || hits.Load() != 1 {
				t.Fatalf("result=%+v manifest=%+v hits=%d", result, manifest, hits.Load())
			}
		})
	}
}
