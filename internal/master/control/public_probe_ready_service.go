package control

import (
	"context"
	"fmt"
	"time"

	"mirror-server/internal/protocol"
)

type pendingPublicProbe struct {
	baseURL   string
	challenge protocol.PublicProbeChallenge
}

func (s *PublicProbeService) issuePending(nodeID, baseURL string, challenge protocol.PublicProbeChallenge) {
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]pendingPublicProbe{}
	}
	s.pending[nodeID] = pendingPublicProbe{
		baseURL:   baseURL,
		challenge: challenge,
	}
	s.mu.Unlock()
	go s.expirePending(nodeID, challenge)
}

func (s *PublicProbeService) expirePending(nodeID string, challenge protocol.PublicProbeChallenge) {
	wait := time.Until(challenge.ExpiresAt)
	if wait > 0 {
		time.Sleep(wait)
	}
	if !s.cancelPending(nodeID, challenge.ChallengeID) {
		return
	}
	thresholdReached, recordErr := s.Repo.RecordPublicProbeNetworkFailure(
		context.Background(), nodeID, s.Config.NetworkFailures,
		"public probe ready timeout")
	s.logProbeFailure(nodeID, fmt.Errorf("public probe ready timeout"),
		recordErr, false, thresholdReached, "ready_timeout", 1)
}

func (s *PublicProbeService) cancelPending(nodeID, challengeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return false
	}
	pending, ok := s.pending[nodeID]
	if !ok || pending.challenge.ChallengeID != challengeID {
		return false
	}
	delete(s.pending, nodeID)
	if s.active != nil {
		delete(s.active, nodeID)
	}
	return true
}

func (s *PublicProbeService) takePendingReady(nodeID, challengeID string) (pendingPublicProbe, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return pendingPublicProbe{}, false
	}
	pending, ok := s.pending[nodeID]
	if !ok || pending.challenge.ChallengeID != challengeID {
		return pendingPublicProbe{}, false
	}
	delete(s.pending, nodeID)
	return pending, true
}

func (s *PublicProbeService) AcceptReady(nodeID string, ready protocol.PublicProbeReady) {
	if s == nil || nodeID == "" || ready.ChallengeID == "" {
		return
	}
	pending, ok := s.takePendingReady(nodeID, ready.ChallengeID)
	if !ok {
		return
	}
	go s.verifyAfterReady(nodeID, pending.baseURL, pending.challenge)
}

func (s *PublicProbeService) verifyAfterReady(nodeID, baseURL string,
	challenge protocol.PublicProbeChallenge) {
	defer s.clearActive(nodeID)
	err, network, attempts := s.verify(nodeID, baseURL, challenge)
	if err == nil {
		s.logProbeRetriedSuccess(nodeID, attempts)
		_ = s.Repo.RecordPublicProbeSuccess(context.Background(), nodeID)
		return
	}
	if network {
		thresholdReached, recordErr := s.Repo.RecordPublicProbeNetworkFailure(
			context.Background(), nodeID, s.Config.NetworkFailures, err.Error())
		s.logProbeFailure(nodeID, err, recordErr, false, thresholdReached, "network", attempts)
		return
	}
	recordErr := s.Repo.RecordPublicProbeAnswerFailure(context.Background(),
		nodeID, err.Error())
	s.logProbeFailure(nodeID, err, recordErr, true, true, "answer", attempts)
}
