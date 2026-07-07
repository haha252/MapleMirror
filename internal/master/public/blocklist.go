package public

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

const maxBlocklistFeedBytes int64 = 4 << 20

type blocklistPolicy struct {
	mu                sync.Mutex
	static            []blocklistEntry
	feeds             []blocklistFeed
	feedItems         map[string][]blocklistEntry
	exemptions        []netip.Prefix
	attempts          map[string]int64
	windows           map[string]*blockedAttemptWindow
	httpClient        *http.Client
	logger            *logging.Logger
	autoBanTTL        time.Duration
	mode              string
	punishmentEnabled bool
	punishmentTotal   int64
	punishmentBurst   int64
	punishmentRolling int64
}

type blocklistEntry struct {
	prefix netip.Prefix
	source string
	note   string
}

type blocklistFeed struct {
	url      string
	interval time.Duration
	timeout  time.Duration
}

type blockDecision struct {
	Blocked          bool
	Reason           string
	Source           string
	Attempts         int64
	Key              string
	BlockedAt        string
	ExpiresAt        string
	EscalationLevel  int
	PunishmentActive bool
}

func newBlocklistPolicy(q config.Quota, logger *logging.Logger) *blocklistPolicy {
	p := &blocklistPolicy{
		feedItems:  map[string][]blocklistEntry{},
		attempts:   map[string]int64{},
		windows:    map[string]*blockedAttemptWindow{},
		httpClient: &http.Client{},
		logger:     logger,
		mode:       strings.ToLower(strings.TrimSpace(q.AbuseControl.Mode)),
		punishmentEnabled: q.AbuseControl.Punishment.Enabled != nil &&
			*q.AbuseControl.Punishment.Enabled,
		punishmentTotal:   int64(q.AbuseControl.Punishment.TotalAttempts),
		punishmentBurst:   int64(q.AbuseControl.Punishment.BurstAttempts),
		punishmentRolling: int64(q.AbuseControl.Punishment.RollingAttempts),
	}
	p.autoBanTTL, _ = time.ParseDuration(q.Blocklist.AutoBanDuration)
	if p.autoBanTTL <= 0 {
		p.autoBanTTL = 7 * 24 * time.Hour
	}
	for _, raw := range q.Blocklist.Static {
		if prefix, err := parseBlockPrefix(raw); err == nil {
			p.static = append(p.static, blocklistEntry{prefix: prefix, source: raw})
		}
	}
	p.exemptions = quotaExemptions(q.Exemptions)
	for _, feed := range q.Blocklist.Feeds {
		interval, _ := time.ParseDuration(feed.RefreshInterval)
		timeout, _ := time.ParseDuration(feed.Timeout)
		if interval <= 0 {
			interval = time.Hour
		}
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		p.feeds = append(p.feeds, blocklistFeed{
			url: strings.TrimSpace(feed.URL), interval: interval, timeout: timeout,
		})
	}
	return p
}

func (p *blocklistPolicy) start() {
	if p == nil {
		return
	}
	for _, feed := range p.feeds {
		go p.refreshLoop(feed)
	}
}

func (p *blocklistPolicy) check(clientPrefix string) blockDecision {
	if p == nil || clientPrefix == "" || clientPrefix == "unknown" {
		return blockDecision{}
	}
	prefix, err := parseBlockPrefix(clientPrefix)
	if err != nil {
		return blockDecision{}
	}
	addr := prefix.Addr()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exemptAddr(addr) {
		return blockDecision{}
	}
	matched, matchedOK := matchBlocklist(p.static, addr)
	reason := "static_blocklist"
	source := matched.source
	for feedURL, entries := range p.feedItems {
		entry, ok := matchBlocklist(entries, addr)
		if !ok || (matchedOK && entry.prefix.Bits() <= matched.prefix.Bits()) {
			continue
		}
		matched, matchedOK = entry, true
		reason = "feed_blocklist"
		source = feedURL + " " + entry.source
	}
	if matchedOK {
		return p.blocked(clientPrefix, reason, source, matched.prefix)
	}
	return blockDecision{}
}

func (p *blocklistPolicy) exempt(clientPrefix string) bool {
	if p == nil || clientPrefix == "" || clientPrefix == "unknown" {
		return false
	}
	prefix, err := parseBlockPrefix(clientPrefix)
	if err != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exemptAddr(prefix.Addr())
}

func (p *blocklistPolicy) autoBanDuration() time.Duration {
	if p == nil || p.autoBanTTL <= 0 {
		return 7 * 24 * time.Hour
	}
	return p.autoBanTTL
}

func (p *blocklistPolicy) exemptAddr(addr netip.Addr) bool {
	return containsPrefix(p.exemptions, addr)
}

func (p *blocklistPolicy) blocked(clientPrefix, reason, source string, matched netip.Prefix) blockDecision {
	p.attempts[clientPrefix]++
	decision := blockDecision{Blocked: true, Reason: reason, Source: source,
		Attempts: p.attempts[clientPrefix], Key: matched.String()}
	if p.mode != "enforce" || !p.punishmentEnabled ||
		(matched.Addr().Is4() && matched.Bits() == 24) {
		return decision
	}
	now := time.Now().UTC()
	window := p.windows[clientPrefix]
	if window == nil {
		window = &blockedAttemptWindow{}
		p.windows[clientPrefix] = window
	}
	window.add(now)
	burst := window.countWindow(now, time.Minute)
	rolling := window.countWindow(now, 10*time.Minute)
	decision.PunishmentActive = decision.Attempts >= p.punishmentTotal ||
		burst >= p.punishmentBurst || rolling >= p.punishmentRolling
	return decision
}

func (p *blocklistPolicy) refreshLoop(feed blocklistFeed) {
	p.refreshFeed(feed)
	ticker := time.NewTicker(feed.interval)
	defer ticker.Stop()
	for range ticker.C {
		p.refreshFeed(feed)
	}
}

func (p *blocklistPolicy) refreshFeed(feed blocklistFeed) {
	ctx, cancel := context.WithTimeout(context.Background(), feed.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.url, nil)
	if err != nil {
		return
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logRefreshError(feed.url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.logRefreshError(feed.url, errors.New(resp.Status))
		return
	}
	entries := parseBlocklistFeed(io.LimitReader(resp.Body, maxBlocklistFeedBytes), feed.url)
	p.mu.Lock()
	p.feedItems[feed.url] = entries
	p.mu.Unlock()
}

func (p *blocklistPolicy) logRefreshError(feedURL string, err error) {
	if p.logger != nil {
		p.logger.Warn(context.Background(), "黑名单订阅刷新失败，继续使用上一次成功快照",
			slog.String("feed_url", feedURL), slog.String("error", err.Error()))
	}
}
