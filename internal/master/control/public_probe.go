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

type PublicProbeService struct {
	Repo   Repository
	Config PublicProbeConfig
	Logger *logging.Logger
	Client *http.Client
	mu     sync.Mutex
	last   map[string]time.Time
	active map[string]bool
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
	go s.verifyAfterGrace(nodeID, baseURL, challenge)
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

func (s *PublicProbeService) verifyAfterGrace(nodeID, baseURL string,
	challenge protocol.PublicProbeChallenge) {
	defer s.clearActive(nodeID)
	time.Sleep(250 * time.Millisecond)
	err, network := s.verify(nodeID, baseURL, challenge)
	if err == nil {
		_ = s.Repo.RecordPublicProbeSuccess(context.Background(), nodeID)
		return
	}
	if network {
		thresholdReached, recordErr := s.Repo.RecordPublicProbeNetworkFailure(
			context.Background(), nodeID, s.Config.NetworkFailures, err.Error())
		s.logProbeFailure(nodeID, err, recordErr, false, thresholdReached, "network")
		return
	}
	recordErr := s.Repo.RecordPublicProbeAnswerFailure(context.Background(),
		nodeID, err.Error())
	s.logProbeFailure(nodeID, err, recordErr, true, true, "answer")
}

func (s *PublicProbeService) verify(nodeID, baseURL string,
	challenge protocol.PublicProbeChallenge) (error, bool) {
	client := publicProbeHTTPClient(s.Client, s.Config.Timeout)
	target, err := publicProbeURL(baseURL, challenge.ChallengeID)
	if err != nil {
		return err, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.Config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err, false
	}
	req.Header.Set("Cache-Control", "no-store")
	resp, err := client.Do(req)
	if err != nil {
		return err, true
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("public probe HTTP status %d", resp.StatusCode), false
	}
	var body protocol.PublicProbeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024)).Decode(&body); err != nil {
		return err, false
	}
	return s.verifyBody(nodeID, challenge, body), false
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
	offline, thresholdReached bool, kind string) {
	if s.Logger == nil {
		return
	}
	attrs := []slog.Attr{slog.String("node_id", nodeID),
		slog.String("kind", kind), slog.Bool("offline", offline),
		slog.Bool("threshold_reached", thresholdReached),
		slog.String("error", probeErr.Error())}
	if recordErr != nil {
		attrs = append(attrs, slog.String("record_error", recordErr.Error()))
	}
	s.Logger.Warn(context.Background(), "节点公网探测失败", attrs...)
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
