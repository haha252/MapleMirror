package control

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

type probeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f probeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPublicProbeAllowsLoopbackSuccess(t *testing.T) {
	restore := stubPublicProbeLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()

	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1",
		Nonce:       "nonce-1",
		ExpiresAt:   time.Now().Add(time.Minute).UTC(),
		Algorithm:   publicprobe.Algorithm,
	}
	signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		challenge.Nonce, challenge.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	targetURL := "http://127.0.0.1:8081"
	expectedTarget, err := publicProbeURL(targetURL, challenge.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}

	service := PublicProbeService{
		Repo:   repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != expectedTarget {
				t.Errorf("unexpected probe URL: %s", req.URL.String())
			}
			if got := req.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("unexpected cache-control header: %q", got)
			}
			data, err := json.Marshal(protocol.PublicProbeResponse{
				NodeID:      "node-1",
				ChallengeID: challenge.ChallengeID,
				Nonce:       challenge.Nonce,
				ExpiresAt:   challenge.ExpiresAt,
				Signature:   signature,
			})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(data))),
				Request:    req,
			}, nil
		})},
	}
	err, network := service.verify("node-1", targetURL, challenge)
	if err != nil || network {
		t.Fatalf("loopback public probe should succeed: err=%v network=%v", err, network)
	}
}
