package control

import (
	"net"
	"time"

	"mirror-server/internal/protocol"
)

const publicProbeReadyMaxAttempts = 3

var publicProbeReadyRetryBackoff = []time.Duration{
	time.Second,
	3 * time.Second,
}

type pendingPublicProbeReady struct {
	ChallengeID   string
	ExpiresAt     time.Time
	Attempts      int
	NextAttemptAt time.Time
}

func (c *Client) enqueuePublicProbeReady(challenge protocol.PublicProbeChallenge) {
	if challenge.ChallengeID == "" {
		return
	}
	now := time.Now().UTC()
	if !challenge.ExpiresAt.After(now) {
		return
	}
	if c.pendingPublicProbeReady == nil {
		c.pendingPublicProbeReady = map[string]pendingPublicProbeReady{}
	}
	c.dropExpiredPendingPublicProbeReady(now)
	pending := pendingPublicProbeReady{
		ChallengeID:   challenge.ChallengeID,
		ExpiresAt:     challenge.ExpiresAt,
		NextAttemptAt: now,
	}
	if current, ok := c.pendingPublicProbeReady[challenge.ChallengeID]; ok {
		pending.Attempts = current.Attempts
		if current.NextAttemptAt.After(now) {
			pending.NextAttemptAt = current.NextAttemptAt
		}
	}
	c.pendingPublicProbeReady[challenge.ChallengeID] = pending
}

func (c *Client) flushPendingPublicProbeReady(conn net.Conn, reqID string,
	sequence uint64, force bool) (uint64, bool, error) {
	now := time.Now().UTC()
	c.dropExpiredPendingPublicProbeReady(now)
	sentAny := false
	for {
		pending, ok := c.nextPendingPublicProbeReady(now, force)
		if !ok {
			return sequence, sentAny, nil
		}
		next, err := c.sendPublicProbeReady(conn, reqID, sequence, pending.ChallengeID)
		if err != nil {
			c.markPendingPublicProbeReadyFailure(pending.ChallengeID, now)
			return sequence, sentAny, err
		}
		delete(c.pendingPublicProbeReady, pending.ChallengeID)
		sequence = next
		sentAny = true
		now = time.Now().UTC()
		c.dropExpiredPendingPublicProbeReady(now)
	}
}

func (c *Client) dropExpiredPendingPublicProbeReady(now time.Time) {
	for challengeID, pending := range c.pendingPublicProbeReady {
		if !pending.ExpiresAt.After(now) {
			delete(c.pendingPublicProbeReady, challengeID)
		}
	}
}

func (c *Client) nextPendingPublicProbeReady(now time.Time, force bool) (pendingPublicProbeReady, bool) {
	var selected pendingPublicProbeReady
	found := false
	for _, pending := range c.pendingPublicProbeReady {
		if pending.Attempts >= publicProbeReadyMaxAttempts {
			continue
		}
		if !force && pending.NextAttemptAt.After(now) {
			continue
		}
		if !found || pending.NextAttemptAt.Before(selected.NextAttemptAt) ||
			(pending.NextAttemptAt.Equal(selected.NextAttemptAt) &&
				pending.ExpiresAt.Before(selected.ExpiresAt)) {
			selected = pending
			found = true
		}
	}
	return selected, found
}

func (c *Client) markPendingPublicProbeReadyFailure(challengeID string, now time.Time) {
	pending, ok := c.pendingPublicProbeReady[challengeID]
	if !ok {
		return
	}
	pending.Attempts++
	if pending.Attempts >= publicProbeReadyMaxAttempts {
		delete(c.pendingPublicProbeReady, challengeID)
		return
	}
	delayIndex := pending.Attempts - 1
	if delayIndex < 0 || delayIndex >= len(publicProbeReadyRetryBackoff) {
		delete(c.pendingPublicProbeReady, challengeID)
		return
	}
	pending.NextAttemptAt = now.Add(publicProbeReadyRetryBackoff[delayIndex])
	c.pendingPublicProbeReady[challengeID] = pending
}
