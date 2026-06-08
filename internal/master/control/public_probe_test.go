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
	"net/http/httptest"
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
		t.Fatalf("second network failure should offline: offline=%v err=%v", offline, err)
	}
	assertProbeNodeState(t, repo, "node-1", "offline", 2)
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
	err, network := service.verify("node-1", "https://node.example.com", challenge)
	if err != nil || network {
		t.Fatalf("signed public probe should verify: err=%v network=%v", err, network)
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
	err, network := service.verify("node-1", "https://node.example.com", challenge)
	if err == nil || network {
		t.Fatalf("wrong nonce should be answer error: err=%v network=%v", err, network)
	}
}

func TestPublicProbeRejectsDNSPrivateAddressBeforeRequest(t *testing.T) {
	restore := stubPublicProbeLookup(t, net.ParseIP("127.0.0.1"))
	defer restore()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"status":"unexpected"}`))
	}))
	defer server.Close()
	service := PublicProbeService{Config: PublicProbeConfig{Timeout: time.Second}}
	challenge := protocol.PublicProbeChallenge{
		ChallengeID: "challenge-1", ExpiresAt: time.Now().Add(time.Minute).UTC(),
	}

	err, network := service.verify("node-1", publicProbePublicURL(t, server.URL), challenge)
	if err == nil || !network || !strings.Contains(err.Error(), "内网") {
		t.Fatalf("expected DNS private network rejection, err=%v network=%v", err, network)
	}
	if hits != 0 {
		t.Fatalf("DNS-private public probe should not reach server, hits=%d", hits)
	}
}

func TestSafePublicProbeDialRejectsPrivateResolvedRanges(t *testing.T) {
	for _, rawIP := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "0.0.0.0"} {
		t.Run(rawIP, func(t *testing.T) {
			restore := stubPublicProbeLookup(t, net.ParseIP(rawIP))
			defer restore()
			_, err := safePublicProbeDialContext(context.Background(), "tcp", "node.example.com:443")
			if err == nil || !strings.Contains(err.Error(), "内网") {
				t.Fatalf("expected private resolved address rejection, got %v", err)
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

type probeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f probeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
