package public

import (
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
)

type abuseLevel int

const (
	abuseLevelNormal abuseLevel = iota
	abuseLevelElevated
	abuseLevelSevere
	abuseLevelReject
)

type abuseDecision struct {
	Level      abuseLevel
	Bits       int
	RetryAfter time.Duration
	ExactBurst int64
	ExactRoll  int64
	NetBurst   int64
	NetRoll    int64
}

type abuseTracker struct {
	mode          string
	cfg           config.AbuseControl
	exactCap      int
	networkCap    int
	mu            sync.Mutex
	exact         map[string]*abuseSeries
	network       map[string]*abuseSeries
	stop          chan struct{}
	startOnce     sync.Once
	closeOnce     sync.Once
}

type abuseSeries struct {
	events   []abuseEvent
	lastUsed time.Time
}

type abuseEvent struct {
	at     time.Time
	weight int64
}

func newAbuseTracker(cfg config.AbuseControl) *abuseTracker {
	return &abuseTracker{
		mode:       strings.ToLower(strings.TrimSpace(cfg.Mode)),
		cfg:        cfg,
		exactCap:   100000,
		networkCap: 20000,
		exact:      map[string]*abuseSeries{},
		network:    map[string]*abuseSeries{},
		stop:       make(chan struct{}),
	}
}

func (t *abuseTracker) start() {
	if t == nil {
		return
	}
	t.startOnce.Do(func() {
		go t.loop()
	})
}

func (t *abuseTracker) close() {
	if t == nil {
		return
	}
	t.closeOnce.Do(func() { close(t.stop) })
}

func (t *abuseTracker) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.cleanup(time.Now().UTC())
		case <-t.stop:
			return
		}
	}
}

func (t *abuseTracker) enabled() bool {
	return t != nil && t.mode != "off"
}

func (t *abuseTracker) record(kind, clientPrefix string, weight int64, now time.Time) {
	if !t.enabled() || weight <= 0 || strings.TrimSpace(clientPrefix) == "" || clientPrefix == "unknown" {
		return
	}
	exactKey, networkKey, ok := abuseKeys(kind, clientPrefix)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if series := t.ensureSeries(t.exact, exactKey, now, t.exactCap); series != nil {
		series.add(now, weight)
	}
	if series := t.ensureSeries(t.network, networkKey, now, t.networkCap); series != nil {
		series.add(now, weight)
	}
}

func (t *abuseTracker) decide(kind, clientPrefix string, now time.Time) abuseDecision {
	if !t.enabled() {
		return abuseDecision{}
	}
	exactKey, networkKey, ok := abuseKeys(kind, clientPrefix)
	if !ok {
		return abuseDecision{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	exact := t.ensureSeries(t.exact, exactKey, now, t.exactCap)
	network := t.ensureSeries(t.network, networkKey, now, t.networkCap)
	decision := abuseDecision{}
	if exact != nil {
		decision.ExactBurst, decision.ExactRoll = exact.counts(now, t.burstWindow(), t.rollingWindow())
	}
	if network != nil {
		decision.NetBurst, decision.NetRoll = network.counts(now, t.burstWindow(), t.rollingWindow())
	}
	decision.Level, decision.Bits, decision.RetryAfter = t.levelFor(decision)
	return decision
}

func (t *abuseTracker) recordAndDecide(kind, clientPrefix string, weight int64, now time.Time) abuseDecision {
	t.record(kind, clientPrefix, weight, now)
	return t.decide(kind, clientPrefix, now)
}

func (t *abuseTracker) ensureSeries(store map[string]*abuseSeries, key string, now time.Time, cap int) *abuseSeries {
	series, ok := store[key]
	if !ok {
		if cap > 0 && len(store) >= cap {
			t.evictOldest(store, now)
			if len(store) >= cap {
				return nil
			}
		}
		series = &abuseSeries{}
		store[key] = series
	}
	series.lastUsed = now
	series.prune(now, t.rollingWindow())
	return series
}

func (t *abuseTracker) evictOldest(store map[string]*abuseSeries, now time.Time) {
	var oldestKey string
	var oldest time.Time
	for key, series := range store {
		if series == nil {
			continue
		}
		if oldestKey == "" || series.lastUsed.Before(oldest) {
			oldestKey = key
			oldest = series.lastUsed
		}
	}
	if oldestKey != "" {
		delete(store, oldestKey)
	}
}

func (t *abuseTracker) cleanup(now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cleanupStore := func(store map[string]*abuseSeries, cap int) {
		for key, series := range store {
			if series == nil {
				delete(store, key)
				continue
			}
			series.prune(now, t.rollingWindow())
			if len(series.events) == 0 && now.Sub(series.lastUsed) > 24*time.Hour {
				delete(store, key)
			}
		}
		for len(store) > cap {
			t.evictOldest(store, now)
		}
	}
	cleanupStore(t.exact, t.exactCap)
	cleanupStore(t.network, t.networkCap)
}

func (t *abuseTracker) burstWindow() time.Duration {
	d, err := time.ParseDuration(t.cfg.Challenge.BurstWindow)
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
}

func (t *abuseTracker) rollingWindow() time.Duration {
	d, err := time.ParseDuration(t.cfg.Challenge.RollingWindow)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}
