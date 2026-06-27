package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
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

func seedPublicProbeNode(t *testing.T, repo Repository, nodeID, baseURL, certPEM string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustExecControl(t, repo.DB, `INSERT INTO nodes
		(id, public_name, certificate_fingerprint, state, target_bandwidth_bps,
		last_heartbeat_at, routing_ready, public_download_base_url, created_at, updated_at)
		VALUES (?, 'node', 'sha256:aa', 'online', 0, ?, 1, ?, ?, ?)`,
		nodeID, now, baseURL, now, now)
	mustExecControl(t, repo.DB, `INSERT INTO node_certificates
		(id, node_id, serial_number, fingerprint, not_before, not_after,
		status, issued_request_id, created_at, certificate_pem) VALUES
		('cert-`+nodeID+`', ?, ?, 'sha256:aa', ?, ?, 'active', 'req', ?, ?)`,
		nodeID, nodeID, now, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
		now, certPEM)
}

func assertProbeNodeState(t *testing.T, repo Repository, nodeID, state string, failures int) {
	t.Helper()
	var gotState string
	var gotFailures, ready int
	err := repo.DB.QueryRow(`SELECT state, public_probe_network_failures,
		routing_ready FROM nodes WHERE id = ?`, nodeID).
		Scan(&gotState, &gotFailures, &ready)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotFailures != failures {
		t.Fatalf("probe state=%s failures=%d ready=%d", gotState, gotFailures, ready)
	}
	if state == "offline" && ready != 0 {
		t.Fatalf("offline public probe node should not be routable: %d", ready)
	}
}

func publicProbeTestCertificate(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1),
		Subject:   pkix.Name{CommonName: "node-1"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return key, string(certPEM)
}

func publicProbeTestClient(body protocol.PublicProbeResponse) *http.Client {
	return &http.Client{Transport: probeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, _ := json.Marshal(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(data))),
			Request:    req,
		}, nil
	})}
}

func stubPublicProbeLookup(t *testing.T, ip net.IP) func() {
	t.Helper()
	previous := publicProbeLookupIPAddr
	publicProbeLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: ip}}, nil
	}
	return func() { publicProbeLookupIPAddr = previous }
}

func stubPublicProbeDial(t *testing.T, dial func(context.Context, string, string) (net.Conn, error)) func() {
	t.Helper()
	previous := publicProbeDialContext
	publicProbeDialContext = dial
	return func() { publicProbeDialContext = previous }
}

func publicProbePublicURL(t *testing.T, serverURL string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	return "http://node.example.test:" + port
}
