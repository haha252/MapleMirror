package public

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

const clientBlockCacheCapacity = 100000

type clientBlockDecision struct {
	Blocked          bool
	Reason           string
	Source           string
	Key              string
	Attempts         int64
	EscalationLevel  int
	PunishmentActive bool
	BlockedAt        string
	ExpiresAt        string
}

type clientBlockManager struct {
	store             *Store
	logger            *logging.Logger
	mode              string
	negativeTTL       time.Duration
	revalidateEvery   time.Duration
	flushInterval     time.Duration
	flushBatch        int64
	escalationLevel1  int64
	escalationLevel2  int64
	escalationLevel3  int64
	escalationDur1    time.Duration
	escalationDur2    time.Duration
	escalationDur3    time.Duration
	punishmentTotal   int64
	punishmentBurst   int64
	punishmentRolling int64
	punishmentEnabled bool

	mu         sync.Mutex
	cache      map[string]clientBlockCacheEntry
	pending    map[string]*pendingBlockFlush
	windows    map[string]*blockedAttemptWindow
	flushLocks [256]sync.Mutex
	stop       chan struct{}
	done       chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once
}

type clientBlockCacheEntry struct {
	record       clientBlockRecord
	fetchedAt    time.Time
	revalidateAt time.Time
	negativeAt   time.Time
}

type pendingBlockFlush struct {
	delta     int64
	lastAt    time.Time
	failCount int
	nextRetry time.Time
}

type blockedAttemptWindow struct {
	events   []time.Time
	start    int
	lastUsed time.Time
}

func newClientBlockManager(db *sql.DB, q config.AbuseControl, logger *logging.Logger) *clientBlockManager {
	flushInterval, _ := time.ParseDuration(q.Blocked.FlushInterval)
	negativeTTL, _ := time.ParseDuration(q.Blocked.CacheNegative)
	level1Dur, _ := time.ParseDuration(q.Blocked.Escalation.Level1Duration)
	level2Dur, _ := time.ParseDuration(q.Blocked.Escalation.Level2Duration)
	level3Dur, _ := time.ParseDuration(q.Blocked.Escalation.Level3Duration)
	if flushInterval <= 0 {
		flushInterval = 30 * time.Second
	}
	if negativeTTL <= 0 {
		negativeTTL = 30 * time.Second
	}
	punishmentEnabled := q.Punishment.Enabled != nil && *q.Punishment.Enabled
	return &clientBlockManager{
		store:             &Store{DB: db},
		logger:            logger,
		mode:              strings.ToLower(strings.TrimSpace(q.Mode)),
		negativeTTL:       negativeTTL,
		revalidateEvery:   5 * time.Minute,
		flushInterval:     flushInterval,
		flushBatch:        int64(max(1, q.Blocked.FlushBatch)),
		escalationLevel1:  int64(q.Blocked.Escalation.Level1Attempts),
		escalationLevel2:  int64(q.Blocked.Escalation.Level2Attempts),
		escalationLevel3:  int64(q.Blocked.Escalation.Level3Attempts),
		escalationDur1:    level1Dur,
		escalationDur2:    level2Dur,
		escalationDur3:    level3Dur,
		punishmentTotal:   int64(q.Punishment.TotalAttempts),
		punishmentBurst:   int64(q.Punishment.BurstAttempts),
		punishmentRolling: int64(q.Punishment.RollingAttempts),
		punishmentEnabled: punishmentEnabled,
		cache:             map[string]clientBlockCacheEntry{},
		pending:           map[string]*pendingBlockFlush{},
		windows:           map[string]*blockedAttemptWindow{},
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
}

func (m *clientBlockManager) start() {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		go m.loop()
	})
}

func (m *clientBlockManager) close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.start()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		close(m.stop)
		select {
		case <-m.done:
			m.flushPending(ctx, time.Now().UTC())
		case <-ctx.Done():
		}
	})
}

func (m *clientBlockManager) loop() {
	defer close(m.done)
	flushTicker := time.NewTicker(m.flushInterval)
	cleanupTicker := time.NewTicker(time.Minute)
	defer flushTicker.Stop()
	defer cleanupTicker.Stop()
	for {
		select {
		case <-flushTicker.C:
			m.flushPending(context.Background(), time.Now().UTC())
		case <-cleanupTicker.C:
			m.cleanup(time.Now().UTC())
		case <-m.stop:
			return
		}
	}
}

func (m *clientBlockManager) invalidate(prefix string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if isIPv4NetworkBlock(prefix) {
		for key, entry := range m.cache {
			if !entry.record.Blocked {
				delete(m.cache, key)
			}
		}
	}
	for _, key := range clientBlockLookupKeys(prefix) {
		delete(m.cache, key)
		delete(m.pending, key)
		delete(m.windows, key)
	}
}
