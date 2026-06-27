package control

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

func TestPublicProbeNetworkFailureNeedsThreshold(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", "")
	offline, err := repo.RecordPublicProbeNetworkFailure(
		context.Background(), "node-1", 2, "dial failed")
	if err != nil || offline {
		t.Fatalf("first network failure should not offline: offline=%v err=%v", offline, err)
	}
	assertProbeNodeState(t, repo, "node-1", "online", 1)
	offline, err = repo.RecordPublicProbeNetworkFailure(
		context.Background(), "node-1", 2, "dial failed again")
	if err != nil || !offline {
		t.Fatalf("second network failure should reach threshold: offline=%v err=%v", offline, err)
	}
	assertProbeNodeState(t, repo, "node-1", "online", 2)
}

func TestPublicProbeAnswerFailureOfflinesImmediately(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	seedPublicProbeNode(t, repo, "node-1", "https://node.example.com", "")
	if err := repo.RecordPublicProbeAnswerFailure(
		context.Background(), "node-1", "signature invalid"); err != nil {
		t.Fatal(err)
	}
	assertProbeNodeState(t, repo, "node-1", "offline", 0)
}

func TestPublicProbeVerifySignedResponse(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(time.Minute).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	signature, err := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		challenge.Nonce, challenge.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	service := PublicProbeService{Repo: repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: publicProbeTestClient(protocol.PublicProbeResponse{
			NodeID: "node-1", ChallengeID: challenge.ChallengeID,
			Nonce: challenge.Nonce, ExpiresAt: challenge.ExpiresAt,
			Signature: signature,
		})}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err != nil || network || attempts != 1 {
		t.Fatalf("signed public probe should verify: err=%v network=%v attempts=%d", err, network, attempts)
	}
}

func TestPublicProbeVerifyRejectsWrongNonceAsAnswerError(t *testing.T) {
	repo, closeDB := testRepo(t)
	defer closeDB()
	key, certPEM := publicProbeTestCertificate(t)
	seedPublicProbeNode(t, repo, "node-1", "", certPEM)
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", Nonce: "nonce-1",
		ExpiresAt: time.Now().Add(time.Minute).UTC(),
		Algorithm: publicprobe.Algorithm,
	}
	signature, _ := publicprobe.Sign(key, "node-1", challenge.ChallengeID,
		"other", challenge.ExpiresAt)
	service := PublicProbeService{Repo: repo,
		Config: PublicProbeConfig{Timeout: time.Second},
		Client: publicProbeTestClient(protocol.PublicProbeResponse{
			NodeID: "node-1", ChallengeID: challenge.ChallengeID,
			Nonce: "other", ExpiresAt: challenge.ExpiresAt,
			Signature: signature,
		})}
	err, network, attempts := service.verify("node-1", "https://node.example.com", challenge)
	if err == nil || network || attempts != 1 {
		t.Fatalf("wrong nonce should be answer error: err=%v network=%v attempts=%d", err, network, attempts)
	}
}

func TestPublicProbeAllowsDNSPrivateAddressToReachDial(t *testing.T) {
	restore := stubPublicProbeLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()
	restoreDial := stubPublicProbeDial(t, func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	})
	defer restoreDial()
	service := PublicProbeService{Config: PublicProbeConfig{Timeout: time.Second}}
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", ExpiresAt: time.Now().Add(time.Minute).UTC(),
	}

	err, network, attempts := service.verify("node-1", "http://public.example.test:8080", challenge)
	if err == nil || !network || attempts != publicProbeVerifyMaxAttempts ||
		strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected DNS private address to reach dial, err=%v network=%v attempts=%d", err, network, attempts)
	}
}

func TestSafePublicProbeDialDoesNotRejectPrivateResolvedRanges(t *testing.T) {
	for _, rawIP := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "0.0.0.0"} {
		t.Run(rawIP, func(t *testing.T) {
			restore := stubPublicProbeLookup(t, net.ParseIP(rawIP))
			defer restore()
			var gotAddress string
			restoreDial := stubPublicProbeDial(t, func(_ context.Context, _ string, address string) (net.Conn, error) {
				gotAddress = address
				client, server := net.Pipe()
				server.Close()
				return client, nil
			})
			defer restoreDial()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			conn, err := safePublicProbeDialContext(ctx, "tcp", "node.example.com:443")
			if err != nil {
				t.Fatalf("should not pre-reject private resolved address, got %v", err)
			}
			if conn != nil {
				conn.Close()
			}
			if gotAddress != net.JoinHostPort(rawIP, "443") {
				t.Fatalf("expected dial to %s, got %q", rawIP, gotAddress)
			}
		})
	}
}
