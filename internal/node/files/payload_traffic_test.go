package files

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/node/activity"
	"mirror-server/internal/swarm"
)

func TestDownloadHandlersSeparatePublicAndNodeUploadBytes(t *testing.T) {
	db, storage, signer := prepareNodeFile(t)
	counters := &activity.Counters{}
	counters.SampleTraffic()
	h := &Handler{DB: db, Storage: storage, NodeID: "node-1", Signer: signer, Activity: counters}
	public, err := signer.Sign(downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1", ClientPrefix: "192.0.2.1/32",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), MaxBytes: 10,
		RangeConcurrencyLimit: 2, RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	peer := swarmToken(t, signer, "asset-1", "manifest-1", "node-1", 6, swarm.MinPieceSize, 1)
	replication := signReplicationToken(t, signer, downloadtoken.ReplicationClaims{
		AssetID: "asset-1", SourceNodeID: "node-1", TargetNodeID: "node-2", TaskID: "task-1",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	for _, tc := range []struct {
		method, path, token, rangeValue string
		status                          int
	}{
		{http.MethodHead, "/downloads/asset-1", public, "", http.StatusOK},
		{http.MethodGet, "/downloads/asset-1", public, "bytes=1-3", http.StatusPartialContent},
		{http.MethodGet, "/downloads/asset-1", public, "bytes=100-200", http.StatusRequestedRangeNotSatisfiable},
		{http.MethodGet, "/internal/swarm/asset-1", peer, "bytes=1-3", http.StatusPartialContent},
		{http.MethodGet, "/internal/swarm/asset-1", "invalid", "bytes=1-3", http.StatusUnauthorized},
		{http.MethodGet, "/internal/replication/asset-1", replication, "", http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.RemoteAddr = "192.0.2.1:12345"
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Range", tc.rangeValue)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s %s: status=%d want=%d body=%s", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
		}
	}
	if counters.PublicDownloads() != 0 || counters.SwarmUploads() != 0 {
		t.Fatal("completed responses must release active counters")
	}
	time.Sleep(time.Second)
	got := counters.SampleTraffic()
	if got.PublicBandwidthBPS != int64(3/got.WindowSeconds) || got.SwarmBandwidthBPS != int64(9/got.WindowSeconds) {
		t.Fatalf("expected public=3 bytes and node uploads=9 bytes, excluding HEAD/error bodies: %+v", got)
	}
}
