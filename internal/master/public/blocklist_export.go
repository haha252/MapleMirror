package public

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

const blocklistExportCacheTTL = time.Minute

type blocklistExportEntry struct {
	Prefix   netip.Prefix
	Reason   string
	Source   string
	Note     string
	Attempts int64
	Blocked  string
	Expires  string
}

type blocklistExportCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	expiresAt time.Time
	entries   []blocklistExportEntry
	body      []byte
}

func newBlocklistExportCache(ttl time.Duration) *blocklistExportCache {
	if ttl <= 0 {
		ttl = blocklistExportCacheTTL
	}
	return &blocklistExportCache{ttl: ttl}
}

func (s Server) blocklistTXT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "INVALID_REQUEST", "请求方法不支持")
		return
	}
	body, err := s.cachedBlocklistTXT(r.Context(), time.Now().UTC())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "PUBLIC_INTERNAL_ERROR", "封禁列表读取失败")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s Server) cachedBlocklistTXT(ctx context.Context, now time.Time) ([]byte, error) {
	_, body, _, err := s.cachedBlocklistPayload(ctx, now)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s Server) cachedBlocklistSnapshot(ctx context.Context, now time.Time) ([]blocklistExportEntry, time.Time, error) {
	entries, _, expiresAt, err := s.cachedBlocklistPayload(ctx, now)
	return entries, expiresAt, err
}

func (s Server) cachedBlocklistPayload(ctx context.Context, now time.Time) ([]blocklistExportEntry, []byte, time.Time, error) {
	cache := s.BlocklistExport
	if cache == nil {
		cache = newBlocklistExportCache(blocklistExportCacheTTL)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.expiresAt.IsZero() && now.Before(cache.expiresAt) {
		return cloneBlocklistExportEntries(cache.entries), append([]byte(nil), cache.body...), cache.expiresAt, nil
	}
	entries, err := s.blocklistExportEntries(ctx, now)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	cache.entries = cloneBlocklistExportEntries(entries)
	cache.body = renderBlocklistTXT(entries)
	cache.expiresAt = now.Add(cache.ttl)
	return cloneBlocklistExportEntries(cache.entries), append([]byte(nil), cache.body...), cache.expiresAt, nil
}

func (s Server) blocklistExportEntries(ctx context.Context, now time.Time) ([]blocklistExportEntry, error) {
	var entries []blocklistExportEntry
	if s.Blocklist != nil {
		entries = append(entries, s.Blocklist.exportEntries()...)
	}
	if s.Store.DB != nil {
		stored, err := s.Store.ActiveClientBlocks(ctx, now)
		if err != nil {
			return nil, err
		}
		entries = append(entries, stored...)
	}
	return mergeBlocklistExportEntries(entries), nil
}

func (p *blocklistPolicy) exportEntries() []blocklistExportEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	var entries []blocklistExportEntry
	for _, item := range p.static {
		entries = append(entries, blocklistExportEntry{
			Prefix: item.prefix, Reason: "static_blocklist", Source: item.source, Note: item.note,
		})
	}
	return entries
}

func (s Store) ActiveClientBlocks(ctx context.Context, now time.Time) ([]blocklistExportEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT client_prefix_key, reason, source,
		attempts_after_block, blocked_at, expires_at FROM client_blocks
		WHERE expires_at > ? ORDER BY client_prefix_key`, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []blocklistExportEntry
	for rows.Next() {
		var raw, reason, source, blocked, expires string
		var attempts int64
		if err := rows.Scan(&raw, &reason, &source, &attempts, &blocked, &expires); err != nil {
			return nil, err
		}
		prefix, err := parseBlockPrefix(raw)
		if err != nil {
			continue
		}
		entries = append(entries, blocklistExportEntry{
			Prefix: prefix, Reason: reason, Source: source, Attempts: attempts,
			Blocked: blocked, Expires: expires,
		})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func mergeBlocklistExportEntries(entries []blocklistExportEntry) []blocklistExportEntry {
	merged := map[string]blocklistExportEntry{}
	for _, entry := range entries {
		if !entry.Prefix.IsValid() {
			continue
		}
		key := entry.Prefix.String()
		current, ok := merged[key]
		if !ok {
			merged[key] = entry
			continue
		}
		current.Reason = joinUnique(current.Reason, entry.Reason)
		current.Source = joinUnique(current.Source, entry.Source)
		current.Note = joinUnique(current.Note, entry.Note)
		if entry.Attempts > current.Attempts {
			current.Attempts = entry.Attempts
		}
		if current.Blocked == "" || (entry.Blocked != "" && entry.Blocked < current.Blocked) {
			current.Blocked = entry.Blocked
		}
		if current.Expires == "" || (entry.Expires != "" && entry.Expires > current.Expires) {
			current.Expires = entry.Expires
		}
		merged[key] = current
	}
	out := make([]blocklistExportEntry, 0, len(merged))
	for _, entry := range merged {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return displayBlockPrefix(out[i].Prefix) < displayBlockPrefix(out[j].Prefix)
	})
	return out
}

func renderBlocklistTXT(entries []blocklistExportEntry) []byte {
	var buf bytes.Buffer
	for _, entry := range entries {
		buf.WriteString(blocklistComment(entry))
		buf.WriteByte('\n')
		buf.WriteString(displayBlockPrefix(entry.Prefix))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func cloneBlocklistExportEntries(entries []blocklistExportEntry) []blocklistExportEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]blocklistExportEntry, len(entries))
	copy(out, entries)
	return out
}

func blocklistComment(entry blocklistExportEntry) string {
	if entry.Note != "" {
		return "# " + entry.Note
	}
	parts := []string{fmt.Sprintf("封禁原因: %s", entry.Reason)}
	if entry.Source != "" {
		parts = append(parts, fmt.Sprintf("来源: %s", entry.Source))
	}
	if entry.Attempts > 0 {
		parts = append(parts, fmt.Sprintf("封禁后尝试次数: %d", entry.Attempts))
	}
	if entry.Expires != "" {
		parts = append(parts, fmt.Sprintf("过期时间: %s", entry.Expires))
	}
	return "# [枫源镜像封禁] " + strings.Join(parts, ", ")
}

func displayBlockPrefix(prefix netip.Prefix) string {
	if !prefix.IsValid() {
		return ""
	}
	if (prefix.Addr().Is4() && prefix.Bits() == 32) || (!prefix.Addr().Is4() && prefix.Bits() == 128) {
		return prefix.Addr().String()
	}
	return prefix.String()
}

func joinUnique(left, right string) string {
	if right == "" || left == right {
		return left
	}
	if left == "" {
		return right
	}
	for _, item := range strings.Split(left, "; ") {
		if item == right {
			return left
		}
	}
	return left + "; " + right
}
