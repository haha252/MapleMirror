package public

import (
	"bufio"
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
	mu         sync.Mutex
	static     []blocklistEntry
	feeds      []blocklistFeed
	feedItems  map[string][]blocklistEntry
	exemptions []netip.Prefix
	attempts   map[string]int64
	httpClient *http.Client
	logger     *logging.Logger
	autoBanTTL time.Duration
}

type blocklistEntry struct {
	prefix netip.Prefix
	source string
}

type blocklistFeed struct {
	url      string
	interval time.Duration
	timeout  time.Duration
}

type blockDecision struct {
	Blocked  bool
	Reason   string
	Source   string
	Attempts int64
}

func newBlocklistPolicy(q config.Quota, logger *logging.Logger) *blocklistPolicy {
	p := &blocklistPolicy{
		feedItems:  map[string][]blocklistEntry{},
		attempts:   map[string]int64{},
		httpClient: &http.Client{},
		logger:     logger,
	}
	p.autoBanTTL, _ = time.ParseDuration(q.Blocklist.AutoBanDuration)
	if p.autoBanTTL <= 0 {
		p.autoBanTTL = 7 * 24 * time.Hour
	}
	for _, raw := range append(q.Blacklist, q.Blocklist.Static...) {
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
	if source, ok := matchBlocklist(p.static, addr); ok {
		return p.blocked(clientPrefix, "static_blocklist", source)
	}
	for feedURL, entries := range p.feedItems {
		if source, ok := matchBlocklist(entries, addr); ok {
			return p.blocked(clientPrefix, "feed_blocklist", feedURL+" "+source)
		}
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

func (p *blocklistPolicy) blocked(clientPrefix, reason, source string) blockDecision {
	p.attempts[clientPrefix]++
	return blockDecision{Blocked: true, Reason: reason, Source: source, Attempts: p.attempts[clientPrefix]}
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

func parseBlocklistFeed(reader io.Reader, _ string) []blocklistEntry {
	var entries []blocklistEntry
	seen := map[string]struct{}{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Split(scanner.Text(), "#")[0])
		if line == "" {
			continue
		}
		if prefix, err := parseBlockPrefix(line); err == nil {
			key := prefix.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, blocklistEntry{prefix: prefix, source: line})
		}
	}
	return entries
}

func parseBlockPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	return netip.PrefixFrom(addr, 128), nil
}

func matchBlocklist(entries []blocklistEntry, addr netip.Addr) (string, bool) {
	for _, entry := range entries {
		if entry.prefix.Contains(addr) {
			return entry.source, true
		}
	}
	return "", false
}

func containsPrefix(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (p *blocklistPolicy) logRefreshError(feedURL string, err error) {
	if p.logger != nil {
		p.logger.Warn(context.Background(), "黑名单订阅刷新失败，继续使用上一次成功快照",
			slog.String("feed_url", feedURL), slog.String("error", err.Error()))
	}
}
