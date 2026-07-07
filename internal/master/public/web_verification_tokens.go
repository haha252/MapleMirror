package public

import (
	"container/list"
	"sync"
	"time"

	"mirror-server/internal/downloadtoken"
)

const (
	webVerificationTokenCapacity = 100000
	webVerificationTokenTTL      = 10 * time.Minute
)

type webVerificationToken struct {
	hash         string
	assetID      string
	clientPrefix string
	issuedAt     time.Time
	expiresAt    time.Time
	element      *list.Element
}

type webVerificationTokenStore struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	entries  map[string]*webVerificationToken
	order    *list.List
	stop     chan struct{}
	done     chan struct{}
	start    sync.Once
	close    sync.Once
}

func newWebVerificationTokenStore(capacity int, ttl time.Duration) *webVerificationTokenStore {
	if capacity <= 0 {
		capacity = webVerificationTokenCapacity
	}
	if ttl <= 0 {
		ttl = webVerificationTokenTTL
	}
	return &webVerificationTokenStore{
		capacity: capacity,
		ttl:      ttl,
		entries:  make(map[string]*webVerificationToken),
		order:    list.New(),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (s *webVerificationTokenStore) startCleanup() {
	if s == nil {
		return
	}
	s.start.Do(func() { go s.cleanupLoop() })
}

func (s *webVerificationTokenStore) closeCleanup() {
	if s == nil {
		return
	}
	s.close.Do(func() {
		s.startCleanup()
		close(s.stop)
		<-s.done
	})
}

func (s *webVerificationTokenStore) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer close(s.done)
	for {
		select {
		case now := <-ticker.C:
			s.cleanup(now.UTC())
		case <-s.stop:
			return
		}
	}
}

func (s *webVerificationTokenStore) issue(assetID, clientPrefix string, now time.Time) (string, error) {
	token, err := downloadtoken.NewOpaque()
	if err != nil {
		return "", err
	}
	hash := downloadtoken.OpaqueHash(token)
	entry := &webVerificationToken{
		hash:         hash,
		assetID:      assetID,
		clientPrefix: clientPrefix,
		issuedAt:     now,
		expiresAt:    now.Add(s.ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	for len(s.entries) >= s.capacity {
		s.removeOldestLocked()
	}
	entry.element = s.order.PushBack(entry)
	s.entries[hash] = entry
	return token, nil
}

func (s *webVerificationTokenStore) consume(token, assetID, clientPrefix string, now time.Time) bool {
	if s == nil || token == "" || assetID == "" || clientPrefix == "" {
		return false
	}
	hash := downloadtoken.OpaqueHash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[hash]
	if !ok || !now.Before(entry.expiresAt) || entry.assetID != assetID || entry.clientPrefix != clientPrefix {
		if ok && !now.Before(entry.expiresAt) {
			s.removeLocked(entry)
		}
		return false
	}
	s.removeLocked(entry)
	return true
}

func (s *webVerificationTokenStore) cleanup(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
}

func (s *webVerificationTokenStore) cleanupLocked(now time.Time) {
	for element := s.order.Front(); element != nil; {
		next := element.Next()
		entry := element.Value.(*webVerificationToken)
		if !now.Before(entry.expiresAt) {
			s.removeLocked(entry)
		}
		element = next
	}
}

func (s *webVerificationTokenStore) removeOldestLocked() {
	if element := s.order.Front(); element != nil {
		s.removeLocked(element.Value.(*webVerificationToken))
	}
}

func (s *webVerificationTokenStore) removeLocked(entry *webVerificationToken) {
	delete(s.entries, entry.hash)
	if entry.element != nil {
		s.order.Remove(entry.element)
		entry.element = nil
	}
}
