package control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

func TestPublicProbeVerifyRetriesTransientNetworkError(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(5 * time.Second).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		challenge.Nonce, challenge.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	service := PublicProbeService{Repo: repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			hits++
			if hits == 1 {
				return nil, context.DeadlineExceeded
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
		})}}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err != nil || network || attempts != 2 || hits != 2 {
		t.Fatalf("transient network error should retry and succeed: err=%v network=%v attempts=%d hits=%d",
			err, network, attempts, hits)
	}
}

func TestPublicProbeVerifyRetriesHTTP5xx(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(5 * time.Second).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		challenge.Nonce, challenge.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	service := PublicProbeService{Repo: repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			hits++
			if hits == 1 {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       io.NopCloser(strings.NewReader("bad gateway")),
					Request:    req,
				}, nil
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
		})}}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err != nil || network || attempts != 2 || hits != 2 {
		t.Fatalf("transient 5xx should retry and succeed: err=%v network=%v attempts=%d hits=%d",
			err, network, attempts, hits)
	}
}

func TestPublicProbeVerifyDoesNotRetryAnswerError(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(5 * time.Second).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		"wrong", challenge.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	service := PublicProbeService{Repo: repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			hits++
			data, _ := json.Marshal(protocol.PublicProbeResponse{
				NodeID: "node-1", ChallengeID: challenge.ChallengeID,
				Nonce: "wrong", ExpiresAt: challenge.ExpiresAt,
				Signature: signature,
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(data))),
				Request:    req,
			}, nil
		})}}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err == nil || network || attempts != 1 || hits != 1 {
		t.Fatalf("answer error should not retry: err=%v network=%v attempts=%d hits=%d",
			err, network, attempts, hits)
	}
}

func TestPublicProbeVerifyStopsWhenTTLTooShortForRetry(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", "")
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(300 * time.Millisecond).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	var hits int
	service := PublicProbeService{
		Repo:   repo,
		Config: PublicProbeConfig{Timeout: 200 * time.Millisecond},
		Client: &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			hits++
			return nil, context.DeadlineExceeded
		})},
	}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err == nil || !network || attempts != 1 || hits != 1 {
		t.Fatalf("short ttl should stop extra retries: err=%v network=%v attempts=%d hits=%d",
			err, network, attempts, hits)
	}
}
