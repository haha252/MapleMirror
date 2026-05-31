package public

import (
	"sync"
	"time"
)

const (
	challengeBucketCapacity = 30
	challengeBucketRefill   = 10 * time.Minute
)

type challengeMemory struct {
	mu      sync.Mutex
	items   map[string]Challenge
	buckets map[string]challengeBucket
	stop    chan struct{}
}

type challengeBucket struct {
	tokens  float64
	updated time.Time
}

func newChallengeMemory() *challengeMemory {
	return &challengeMemory{
		items:   map[string]Challenge{},
		buckets: map[string]challengeBucket{},
		stop:    make(chan struct{}),
	}
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
		delete(m.items, id)
		return Challenge{}, false
	}
	return challenge, true
}

func (m *challengeMemory) consume(id string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	challenge, ok := m.items[id]
	if !ok || challengeExpired(challenge, now) {
		delete(m.items, id)
		return false
	}
	delete(m.items, id)
	return true
}

func (m *challengeMemory) allow(kind, prefix string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := kind + "|" + prefix
	bucket := m.buckets[key]
	if bucket.updated.IsZero() {
		bucket = challengeBucket{tokens: challengeBucketCapacity, updated: now}
	}
	elapsed := now.Sub(bucket.updated)
	bucket.tokens += float64(challengeBucketCapacity) * elapsed.Seconds() / challengeBucketRefill.Seconds()
	if bucket.tokens > challengeBucketCapacity {
		bucket.tokens = challengeBucketCapacity
	}
	bucket.updated = now
	if bucket.tokens < 1 {
		m.buckets[key] = bucket
		return false
	}
	bucket.tokens--
	m.buckets[key] = bucket
	return true
}

func (m *challengeMemory) cleanup(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
}

func (m *challengeMemory) cleanupLocked(now time.Time) {
	for id, challenge := range m.items {
		if challengeExpired(challenge, now) {
			delete(m.items, id)
		}
	}
	for key, bucket := range m.buckets {
		if now.Sub(bucket.updated) > challengeBucketRefill {
			delete(m.buckets, key)
		}
	}
}

func challengeExpired(challenge Challenge, now time.Time) bool {
	expires, err := time.Parse(time.RFC3339Nano, challenge.ExpiresAt)
	return err != nil || !now.Before(expires)
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
