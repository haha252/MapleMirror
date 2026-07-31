package public

import (
	"sync"
	"time"

	"mirror-server/internal/config"
)

const challengeBucketCapacity = 30

type challengeMemory struct {
	mu          sync.Mutex
	items       map[string]Challenge
	pending     map[string]struct{}
	buckets     map[string]challengeBucket
	outstanding map[string]int
	total       int
	limits      config.ChallengeLimits
	stop        chan struct{}
	closeOnce   sync.Once
}

type challengeBucket struct {
	tokens  float64
	updated time.Time
}

func newChallengeMemory(values ...config.ChallengeLimits) *challengeMemory {
	limits := config.ChallengeLimits{BucketCapacity: challengeBucketCapacity, BucketFullRefill: "10m",
		MaxOutstandingExact: 4, MaxOutstandingTotal: 100000}
	if len(values) > 0 {
		limits = values[0]
	}
	return &challengeMemory{items: map[string]Challenge{}, pending: map[string]struct{}{},
		buckets: map[string]challengeBucket{}, outstanding: map[string]int{},
		limits: limits, stop: make(chan struct{})}
}

func (m *challengeMemory) reserve(prefix string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	refill, _ := time.ParseDuration(m.limits.BucketFullRefill)
	bucket := m.buckets[prefix]
	if bucket.updated.IsZero() {
		bucket = challengeBucket{tokens: float64(m.limits.BucketCapacity), updated: now}
	}
	bucket.tokens += float64(m.limits.BucketCapacity) * now.Sub(bucket.updated).Seconds() / refill.Seconds()
	if bucket.tokens > float64(m.limits.BucketCapacity) {
		bucket.tokens = float64(m.limits.BucketCapacity)
	}
	bucket.updated = now
	if bucket.tokens < 1 {
		m.buckets[prefix] = bucket
		return errChallengeQuota
	}
	bucket.tokens--
	m.buckets[prefix] = bucket
	if m.outstanding[prefix] >= m.limits.MaxOutstandingExact {
		return errChallengeOutstanding
	}
	if m.total >= m.limits.MaxOutstandingTotal {
		return errChallengeCapacity
	}
	m.outstanding[prefix]++
	m.total++
	return nil
}

func (m *challengeMemory) releaseReservation(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseOutstandingLocked(prefix)
}

func (m *challengeMemory) put(challenge Challenge, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[challenge.ID] = challenge
	m.cleanupLocked(now)
}

func (m *challengeMemory) get(id string, now time.Time) (Challenge, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	challenge, ok := m.items[id]
	if !ok || challengeExpired(challenge, now) {
		m.deleteLocked(id)
		return Challenge{}, false
	}
	return challenge, true
}

func (m *challengeMemory) begin(id string, now time.Time) (Challenge, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	challenge, ok := m.items[id]
	if !ok || challengeExpired(challenge, now) {
		m.deleteLocked(id)
		return Challenge{}, false, false
	}
	if _, busy := m.pending[id]; busy {
		return Challenge{}, true, true
	}
	m.pending[id] = struct{}{}
	return challenge, true, false
}

func (m *challengeMemory) finish(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteLocked(id)
}

func (m *challengeMemory) release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, id)
}

func (m *challengeMemory) deleteLocked(id string) {
	challenge, ok := m.items[id]
	if ok {
		m.releaseOutstandingLocked(challenge.ClientPrefixKey)
	}
	delete(m.items, id)
	delete(m.pending, id)
}

func (m *challengeMemory) releaseOutstandingLocked(prefix string) {
	if m.outstanding[prefix] <= 0 {
		return
	}
	m.outstanding[prefix]--
	m.total--
	if m.outstanding[prefix] == 0 {
		delete(m.outstanding, prefix)
	}
}

func (m *challengeMemory) cleanup(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
}

func (m *challengeMemory) cleanupLocked(now time.Time) {
	for id, challenge := range m.items {
		if challengeExpired(challenge, now) {
			m.deleteLocked(id)
		}
	}
	refill, _ := time.ParseDuration(m.limits.BucketFullRefill)
	for key, bucket := range m.buckets {
		if now.Sub(bucket.updated) > refill {
			delete(m.buckets, key)
		}
	}
}

func challengeExpired(challenge Challenge, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, challenge.ExpiresAt)
	return err != nil || !now.Before(expires)
}

func minDuration(values ...time.Duration) time.Duration {
	out := time.Duration(0)
	for _, value := range values {
		if value > 0 && (out == 0 || value < out) {
			out = value
		}
	}
	return out
}

func (m *challengeMemory) startCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.cleanup(time.Now().UTC())
			case <-m.stop:
				return
			}
		}
	}()
}

func (m *challengeMemory) Close() { m.closeOnce.Do(func() { close(m.stop) }) }
