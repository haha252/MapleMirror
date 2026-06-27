package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/logging"
	"mirror-server/internal/protocol"
	"mirror-server/internal/publicprobe"
)

type PublicProbeConfig struct {
	Enabled         bool
	Interval        time.Duration
	Timeout         time.Duration
	TTL             time.Duration
	NetworkFailures int
}

const publicProbeVerifyMaxAttempts = 3

var publicProbeVerifyRetryBackoff = []time.Duration{
	250 * time.Millisecond,
	time.Second,
}

type PublicProbeService struct {
	Repo    Repository
	Config  PublicProbeConfig
	Logger  *logging.Logger
	Client  *http.Client
	mu      sync.Mutex
	last    map[string]time.Time
	active  map[string]bool
	pending map[string]pendingPublicProbe
}

func (s *PublicProbeService) ChallengeForHeartbeat(nodeID string) *protocol.PublicProbeChallenge {
	if s == nil || !s.Config.Enabled || nodeID == "" {
		return nil
	}
	now := time.Now().UTC()
	if !s.shouldIssue(nodeID, now) {
		return nil
	}
	baseURL, err := s.Repo.PublicProbeBaseURL(context.Background(), nodeID)
	if err != nil || baseURL == "" {
		s.clearActive(nodeID)
		return nil
	}
	challenge, err := newPublicProbeChallenge(now, s.Config.TTL)
	if err != nil {
		s.clearActive(nodeID)
		return nil
	}
	s.issuePending(nodeID, baseURL, challenge)
	return &challenge
}

func (s *PublicProbeService) shouldIssue(nodeID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]time.Time{}
	}
	if s.active == nil {
		s.active = map[string]bool{}
	}
	if s.active[nodeID] {
		return false
	}
	if last, ok := s.last[nodeID]; ok && now.Sub(last) < s.Config.Interval {
		return false
	}
	s.last[nodeID] = now
	s.active[nodeID] = true
	return true
}

func (s *PublicProbeService) clearActive(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		delete(s.active, nodeID)
	}
}

func (s *PublicProbeService) verify(nodeID, baseURL string,
	challenge protocol.PublicProbeChallenge) (error, bool, int) {
	attempts := 0
	for {
		attempts++
		err, network, retryable := s.verifyOnce(nodeID, baseURL, challenge)
		if err == nil {
			return nil, false, attempts
		}
		if !retryable || !s.shouldRetryVerify(challenge.ExpiresAt, attempts) {
			return err, network, attempts
		}
		time.Sleep(publicProbeVerifyRetryBackoff[attempts-1])
	}
}

func (s *PublicProbeService) verifyOnce(nodeID, baseURL string,
	challenge protocol.PublicProbeChallenge) (error, bool, bool) {
	client := publicProbeHTTPClient(s.Client, s.Config.Timeout)
	target, err := publicProbeURL(baseURL, challenge.ChallengeID)
	if err != nil {
		return err, false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.Config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err, false, false
	}
	req.Header.Set("Cache-Control", "no-store")
	resp, err := client.Do(req)
	if err != nil {
		return err, true, true
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("public probe HTTP status %d", resp.StatusCode), true, true
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("public probe HTTP status %d", resp.StatusCode), false, false
	}
	var body protocol.PublicProbeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024)).Decode(&body); err != nil {
		return err, false, false
	}
	return s.verifyBody(nodeID, challenge, body), false, false
}

func (s *PublicProbeService) shouldRetryVerify(expiresAt time.Time, attempts int) bool {
	if attempts >= publicProbeVerifyMaxAttempts || attempts > len(publicProbeVerifyRetryBackoff) {
		return false
	}
	retryDeadline := time.Now().UTC().Add(publicProbeVerifyRetryBackoff[attempts-1] + s.Config.Timeout)
	return retryDeadline.Before(expiresAt)
}

func (s *PublicProbeService) verifyBody(nodeID string,
	challenge protocol.PublicProbeChallenge, body protocol.PublicProbeResponse) error {
	if body.NodeID != nodeID || body.ChallengeID != challenge.ChallengeID ||
		body.Nonce != challenge.Nonce || !body.ExpiresAt.Equal(challenge.ExpiresAt) {
		return fmt.Errorf("public probe response fields mismatch")
	}
	if !time.Now().UTC().Before(body.ExpiresAt) {
		return fmt.Errorf("public probe response expired")
	}
	cert, err := s.Repo.ActiveNodeCertificate(context.Background(), nodeID)
	if err != nil {
		return err
	}
	if !publicprobe.Verify(publicprobe.PublicKeyFromCert(cert), body.Signature,
		body.NodeID, body.ChallengeID, body.Nonce, body.ExpiresAt) {
		return fmt.Errorf("public probe signature invalid")
	}
	return nil
}

func (s *PublicProbeService) logProbeFailure(nodeID string, probeErr, recordErr error,
	offline, thresholdReached bool, kind string, attempts int) {
	if s.Logger == nil {
		return
	}
	attrs := []slog.Attr{slog.String("node_id", nodeID),
		slog.String("kind", kind), slog.Bool("offline", offline),
		slog.Bool("threshold_reached", thresholdReached),
		slog.Int("attempts", attempts), slog.String("error", probeErr.Error())}
	if recordErr != nil {
		attrs = append(attrs, slog.String("record_error", recordErr.Error()))
	}
	s.Logger.Warn(context.Background(), "节点公网探测失败", attrs...)
}

func (s *PublicProbeService) logProbeRetriedSuccess(nodeID string, attempts int) {
	if s.Logger == nil || attempts <= 1 {
		return
	}
	s.Logger.Debug(context.Background(), "节点公网探测重试后成功",
		slog.String("node_id", nodeID),
		slog.String("kind", "network"),
		slog.Int("attempts", attempts))
}

func newPublicProbeChallenge(now time.Time, ttl time.Duration) (protocol.PublicProbeChallenge, error) {
	id, err := newID()
	if err != nil {
		return protocol.PublicProbeChallenge{}, err
	}
	nonce, err := secureToken(32)
	if err != nil {
		return protocol.PublicProbeChallenge{}, err
	}
	return protocol.PublicProbeChallenge{ChallengeID: id, Nonce: nonce,
		ExpiresAt: now.Add(ttl), Algorithm: publicprobe.Algorithm}, nil
}

func publicProbeURL(baseURL, challengeID string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("public probe base URL is invalid")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") +
		"/.well-known/mirror-node/probes/" + url.PathEscape(challengeID)
	return parsed.String(), nil
}
