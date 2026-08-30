package developerapi

import (
	"sync"
	"time"
)

const (
	authFailureWindow   = 10 * time.Minute
	authFailureLimit    = 20
	authBlockDuration   = 30 * time.Minute
	authFailureCapacity = 4096
)

type authFailureState struct {
	windowStart time.Time
	count       int
	blockedTill time.Time
}

type authFailureLimiter struct {
	mu    sync.Mutex
	items map[string]authFailureState
}

func newAuthFailureLimiter() *authFailureLimiter {
	return &authFailureLimiter{items: map[string]authFailureState{}}
}

func (l *authFailureLimiter) blocked(key string, now time.Time) (bool, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.items[key]
	if item.blockedTill.After(now) {
		return true, item.blockedTill
	}
	if !item.blockedTill.IsZero() {
		delete(l.items, key)
	}
	return false, time.Time{}
}

func (l *authFailureLimiter) failure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.items) >= authFailureCapacity {
		l.cleanupExpiredLocked(now)
		if _, exists := l.items[key]; !exists && len(l.items) >= authFailureCapacity {
			return
		}
	}
	item := l.items[key]
	if item.windowStart.IsZero() || now.Sub(item.windowStart) >= authFailureWindow {
		item = authFailureState{windowStart: now}
	}
	item.count++
	if item.count >= authFailureLimit {
		item.blockedTill = now.Add(authBlockDuration)
	}
	l.items[key] = item
}

func (l *authFailureLimiter) success(key string) {
	l.mu.Lock()
	delete(l.items, key)
	l.mu.Unlock()
}

func (l *authFailureLimiter) cleanupExpiredLocked(now time.Time) {
	for key, item := range l.items {
		if item.blockedTill.After(now) {
			continue
		}
		if item.windowStart.IsZero() || now.Sub(item.windowStart) >= authFailureWindow {
			delete(l.items, key)
		}
	}
}
