package files

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
)

func TestHandlerServesPublicProbeResponse(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	expires := time.Now().Add(time.Minute).UTC()
	store := staticProbeStore{response: protocol.PublicProbeResponse{
		NodeID: "node-1", ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: expires, Signature: "sig-1",
	}}
	req := httptest.NewRequest(http.MethodGet,
		"/.well-known/mirror-node/probes/challenge-1", nil)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer, ProbeStore: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public probe response code = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("public probe cache header = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"signature":"sig-1"`) {
		t.Fatalf("public probe body mismatch: %s", rec.Body.String())
	}
}

func TestHandlerRejectsUnknownPublicProbe(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	req := httptest.NewRequest(http.MethodGet,
		"/.well-known/mirror-node/probes/missing", nil)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer, ProbeStore: staticProbeStore{}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown public probe should be 404: %d", rec.Code)
	}
}

type staticProbeStore struct {
	response protocol.PublicProbeResponse
}

func (s staticProbeStore) Response(id string) (protocol.PublicProbeResponse, bool) {
	if s.response.ChallengeID != id {
		return protocol.PublicProbeResponse{}, false
	}
	return s.response, true
}
