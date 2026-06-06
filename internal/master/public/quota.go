package public

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"mirror-server/internal/config"
)

const tokenUnit int64 = 1_000_000

var (
	errRequestQuota = errors.New("请求额度不足")
	errTrafficLimit = errors.New("每日流量额度不足")
)

type quotaPolicy struct {
	buckets    map[string]bucketRule
	daily      map[string]int64
	exemptions []netip.Prefix
}

type bucketRule struct {
	capacity int64
	refill   time.Duration
}

type quotaScope struct {
	Kind string
	Key  string
}

func newQuotaPolicy(q config.Quota) quotaPolicy {
	return quotaPolicy{
		buckets: map[string]bucketRule{
			"ipv4_32":  rule(q.RequestBuckets.IPv432),
			"ipv4_24":  rule(q.RequestBuckets.IPv424),
			"ipv6_128": rule(q.RequestBuckets.IPv6128),
			"ipv6_64":  rule(q.RequestBuckets.IPv664),
		},
		daily: map[string]int64{
			"ipv4_32":  gib(q.DailyTraffic.IPv432),
			"ipv4_24":  gib(q.DailyTraffic.IPv424),
			"ipv6_128": gib(q.DailyTraffic.IPv6128),
			"ipv6_64":  gib(q.DailyTraffic.IPv664),
		},
		exemptions: quotaExemptions(q.Exemptions),
	}
}

func rule(b config.Bucket) bucketRule {
	refill, _ := time.ParseDuration(b.FullRefill)
	if b.Capacity <= 0 {
		b.Capacity = 120
	}
	if refill <= 0 {
		refill = 48 * time.Hour
	}
	return bucketRule{capacity: int64(b.Capacity) * tokenUnit, refill: refill}
}

func gib(value string) int64 {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return 3 << 30
	}
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || n <= 0 {
		return 3 << 30
	}
	return n << 30
}

func defaultQuota() quotaPolicy {
	return newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: 120, FullRefill: "48h"},
			IPv424:  config.Bucket{Capacity: 600, FullRefill: "48h"},
			IPv6128: config.Bucket{Capacity: 120, FullRefill: "48h"},
			IPv664:  config.Bucket{Capacity: 600, FullRefill: "48h"},
		},
		DailyTraffic: config.DailyTraffic{
			IPv432: "3 GiB", IPv424: "20 GiB", IPv6128: "3 GiB", IPv664: "20 GiB",
		},
		AuthorizationMaxBytesMultiplier: 2,
	})
}

func quotaScopes(prefix string) ([2]quotaScope, error) {
	host := strings.TrimSuffix(strings.TrimSuffix(prefix, "/32"), "/128")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return [2]quotaScope{}, err
	}
	if addr.Is4() {
		p24 := netip.PrefixFrom(addr, 24).Masked().String()
		return [2]quotaScope{{"ipv4_32", addr.String() + "/32"}, {"ipv4_24", p24}}, nil
	}
	p64 := netip.PrefixFrom(addr, 64).Masked().String()
	return [2]quotaScope{{"ipv6_128", addr.String() + "/128"}, {"ipv6_64", p64}}, nil
}

func (p quotaPolicy) consume(ctx context.Context, tx *sql.Tx, scopes [2]quotaScope, multiplier int64, now time.Time) error {
	for _, scope := range scopes {
		if err := p.consumeOne(ctx, tx, scope, multiplier*tokenUnit, now); err != nil {
			return err
		}
	}
	return nil
}

func (p quotaPolicy) consumeOne(ctx context.Context, tx *sql.Tx, scope quotaScope, need int64, now time.Time) error {
	rule := p.buckets[scope.Kind]
	var tokens int64
	var updatedText string
	err := tx.QueryRowContext(ctx, `SELECT tokens_microunits, updated_at FROM quota_buckets
		WHERE scope_kind = ? AND scope_key = ?`, scope.Kind, scope.Key).Scan(&tokens, &updatedText)
	if err == sql.ErrNoRows {
		tokens, updatedText = rule.capacity, now.Format(time.RFC3339Nano)
	} else if err != nil {
		return err
	}
	updated, _ := time.Parse(time.RFC3339Nano, updatedText)
	if !updated.IsZero() && rule.refill > 0 {
		add := int64(float64(rule.capacity) * now.Sub(updated).Seconds() / rule.refill.Seconds())
		tokens = min64(rule.capacity, tokens+add)
	}
	if tokens < need {
		return errRequestQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO quota_buckets
		(scope_kind, scope_key, tokens_microunits, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(scope_kind, scope_key) DO UPDATE SET
		tokens_microunits = excluded.tokens_microunits, updated_at = excluded.updated_at`,
		scope.Kind, scope.Key, tokens-need, now.Format(time.RFC3339Nano))
	return err
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func statDay(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02")
}

func (p quotaPolicy) reserve(ctx context.Context, tx *sql.Tx, day string, scopes [2]quotaScope) error {
	for _, scope := range scopes {
		var accounted int64
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_bytes), 0)
			FROM daily_traffic_stats WHERE stat_day = ? AND scope_kind = ?
			AND scope_key = ?`, day, scope.Kind, scope.Key).Scan(&accounted)
		if err != nil {
			return err
		}
		if accounted >= p.daily[scope.Kind] {
			return errTrafficLimit
		}
	}
	return nil
}

func (p quotaPolicy) snapshot(ctx context.Context, tx *sql.Tx, day string, scopes [2]quotaScope) (map[string]int64, map[string]int64, error) {
	requestRemaining := make(map[string]int64, len(scopes))
	trafficRemaining := make(map[string]int64, len(scopes))
	for _, scope := range scopes {
		var tokens int64
		if err := tx.QueryRowContext(ctx, `SELECT tokens_microunits FROM quota_buckets
			WHERE scope_kind = ? AND scope_key = ?`, scope.Kind, scope.Key).Scan(&tokens); err != nil {
			return nil, nil, err
		}
		requestRemaining[scope.Kind] = tokens
		var accounted int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_bytes), 0)
			FROM daily_traffic_stats WHERE stat_day = ? AND scope_kind = ?
			AND scope_key = ?`, day, scope.Kind, scope.Key).Scan(&accounted); err != nil {
			return nil, nil, err
		}
		remaining := p.daily[scope.Kind] - accounted
		if remaining < 0 {
			remaining = 0
		}
		trafficRemaining[scope.Kind] = remaining
	}
	return requestRemaining, trafficRemaining, nil
}
