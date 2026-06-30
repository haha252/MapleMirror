package adminui

import (
	"errors"
	"sync"
	"time"
)

type loginMemory struct {
	mu       sync.Mutex
	sessions map[string]memorySession
	failures map[string]memoryFailure
	blocks   map[string]memoryBlock
}

type memorySession struct {
	Username  string
	IPKey     string
	ExpiresAt string
}

type memoryFailure struct {
	MaskedIP      string
	FailedCount   int
	WindowStarted time.Time
	LastFailedAt  string
}

type memoryBlock struct {
	MaskedIP           string
	DisplayIP          string
	BlockedAt          string
	ExpiresAt          string
	AttemptsAfterBlock int
	LastAttemptAt      string
}

func newLoginMemory() *loginMemory {
	return &loginMemory{
		sessions: map[string]memorySession{},
		failures: map[string]memoryFailure{},
		blocks:   map[string]memoryBlock{},
	}
}

func (m *loginMemory) blocked(key, now string) (blockStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	block, ok := m.blocks[key]
	if !ok || block.ExpiresAt <= now {
		delete(m.blocks, key)
		return blockStatus{}, false
	}
	block.AttemptsAfterBlock++
	block.LastAttemptAt = now
	m.blocks[key] = block
	return blockStatus{Blocked: true, MaskedIP: block.MaskedIP, ExpiresAt: block.ExpiresAt}, true
}

func (m *loginMemory) recordFailure(key, masked, display string, window time.Duration, limit int, banDuration time.Duration) {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	m.mu.Lock()
	defer m.mu.Unlock()
	failure := m.failures[key]
	if failure.WindowStarted.IsZero() || now.Sub(failure.WindowStarted) > window {
		failure = memoryFailure{MaskedIP: masked, FailedCount: 1, WindowStarted: now, LastFailedAt: nowText}
	} else {
		failure.MaskedIP = masked
		failure.FailedCount++
		failure.LastFailedAt = nowText
	}
	m.failures[key] = failure
	if failure.FailedCount >= limit {
		m.blocks[key] = memoryBlock{
			MaskedIP:      masked,
			DisplayIP:     display,
			BlockedAt:     nowText,
			ExpiresAt:     now.Add(banDuration).Format(time.RFC3339Nano),
			LastAttemptAt: nowText,
		}
	}
}

type memoryBlockItem struct {
	Key                string
	DisplayIP          string
	MaskedIP           string
	BlockedAt          string
	ExpiresAt          string
	AttemptsAfterBlock int
	LastAttemptAt      string
}

func (m *loginMemory) activeBlocks(now string) []memoryBlockItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []memoryBlockItem{}
	for key, block := range m.blocks {
		if block.ExpiresAt <= now {
			delete(m.blocks, key)
			continue
		}
		out = append(out, memoryBlockItem{
			Key: key, DisplayIP: block.DisplayIP, MaskedIP: block.MaskedIP,
			BlockedAt: block.BlockedAt, ExpiresAt: block.ExpiresAt,
			AttemptsAfterBlock: block.AttemptsAfterBlock,
			LastAttemptAt:      block.LastAttemptAt,
		})
	}
	return out
}

func (m *loginMemory) clearFailures(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failures, key)
}

func (m *loginMemory) createSession(id, username, ipKey, expires string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = memorySession{Username: username, IPKey: ipKey, ExpiresAt: expires}
}

func (m *loginMemory) verifySession(id, ipKey, now string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.ExpiresAt <= now {
		delete(m.sessions, id)
		return "", false, nil
	}
	if session.IPKey != ipKey {
		return "", false, errors.New("管理面板会话来源不匹配")
	}
	return session.Username, true, nil
}

func (m *loginMemory) deleteSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}
