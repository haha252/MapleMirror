package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

func TestPublicProbeWaitsForReadyBeforeVerify(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	_, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", certPEM)
	var requests int32
	service := PublicProbeService{
		Repo: repo,
		Config: PublicProbeConfig{
			Enabled: true, Interval: time.Second, Timeout: time.Second, TTL: time.Second,
		},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&requests, 1)
			return nil, context.DeadlineExceeded
		})},
	}

	challenge := service.ChallengeForHeartbeat("node-1")
	if challenge == nil {
		t.Fatal("expected public probe challenge")
	}
	time.Sleep(400 * time.Millisecond)
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("public probe should wait for ready before verify, got %d requests", got)
	}
}

func TestPublicProbeReadyTimeoutCountsAsNetworkFailure(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	_, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", certPEM)
	service := PublicProbeService{
		Repo: repo,
		Config: PublicProbeConfig{
			Enabled:         true,
			Interval:        time.Second,
			Timeout:         time.Second,
			TTL:             20 * time.Millisecond,
			NetworkFailures: 2,
		},
	}

	challenge := service.ChallengeForHeartbeat("node-1")
	if challenge == nil {
		t.Fatal("expected public probe challenge")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var state, result, message string
		var failures int
		err := repo.DB.QueryRow(`SELECT state, last_public_probe_result,
			COALESCE(last_public_probe_error, ''), public_probe_network_failures
			FROM nodes WHERE id = ?`, "node-1").
			Scan(&state, &result, &message, &failures)
		if err == nil && result == "network_error" {
			if state != "online" || failures != 1 || message != "public probe ready timeout" {
				t.Fatalf("ready timeout state=%s result=%s failures=%d message=%q",
					state, result, failures, message)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected ready timeout to be recorded")
}

func TestPublicProbeReadyTriggersVerify(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", certPEM)
	var requests int32
	var challenge *protocol.PublicProbeChallenge
	service := PublicProbeService{
		Repo: repo,
		Config: PublicProbeConfig{
			Enabled: true, Interval: time.Second, Timeout: time.Second, TTL: 2 * time.Second,
		},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&requests, 1)
			if challenge == nil {
				return nil, errors.New("challenge missing")
			}
			signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
				challenge.Nonce, challenge.ExpiresAt)
			if err != nil {
				return nil, err
			}
			data, _ := json.Marshal(protocol.PublicProbeResponse{
				NodeID: "node-1", ChallengeID: challenge.ChallengeID,
				Nonce: challenge.Nonce, ExpiresAt: challenge.ExpiresAt,
				Signature: signature,
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(data))),
				Request:    req,
			}, nil
		})},
	}

	challenge = service.ChallengeForHeartbeat("node-1")
	if challenge == nil {
		t.Fatal("expected public probe challenge")
	}
	service.AcceptReady("node-1", protocol.PublicProbeReady{
		ChallengeID: challenge.ChallengeID,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&requests) > 0 {
			var result string
			var failures int
			err := repo.DB.QueryRow(`SELECT last_public_probe_result,
				public_probe_network_failures FROM nodes WHERE id = ?`, "node-1").
				Scan(&result, &failures)
			if err == nil && result == "success" && failures == 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected public probe verify after ready")
}
