package public

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"mirror-server/internal/config"
)

type publicResourceLimiter struct {
	mu         sync.Mutex
	rules      map[string]bucketRule
	buckets    map[resourceScope]resourceBucket
	exemptions []netip.Prefix
}

type resourceScope struct {
	Kind string
	Key  string
}

type resourceBucket struct {
	tokens  float64
	updated time.Time
}

type resourceLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

func newPublicResourceLimiter(q config.Quota) *publicResourceLimiter {
	return &publicResourceLimiter{
		rules: map[string]bucketRule{
			"ipv4_32":         resourceRule(q.PublicResourceBuckets.IPv432, 3600),
			"ipv4_24":         resourceRule(q.PublicResourceBuckets.IPv424, 10800),
			"ipv6_128":        resourceRule(q.PublicResourceBuckets.IPv6128, 3600),
			"ipv6_64":         resourceRule(q.PublicResourceBuckets.IPv664, 10800),
			"unknown_address": resourceRule(q.PublicResourceBuckets.IPv432, 3600),
			"unknown_network": resourceRule(q.PublicResourceBuckets.IPv424, 10800),
		},
		buckets:    map[resourceScope]resourceBucket{},
		exemptions: quotaExemptions(q.Exemptions),
	}
}

func resourceRule(b config.Bucket, fallback int) bucketRule {
	refill, _ := time.ParseDuration(b.FullRefill)
	if b.Capacity <= 0 {
		b.Capacity = fallback
	}
	if refill <= 0 {
		refill = time.Hour
	}
	return bucketRule{capacity: int64(b.Capacity), refill: refill}
}

func (l *publicResourceLimiter) middleware(next http.Handler, trusted []string) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := clientPrefixFromRequest(r, trusted)
		decision := l.allow(prefix, time.Now().UTC())
		if decision.Allowed {
			next.ServeHTTP(w, r)
			return
		}
		seconds := int(math.Ceil(decision.RetryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, r, http.StatusTooManyRequests, "PUBLIC_RESOURCE_RATE_LIMITED", "公共资源请求过于频繁，请稍后再试")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("公共资源请求过于频繁，请稍后再试\n"))
	})
}

func (l *publicResourceLimiter) allow(clientPrefix string, now time.Time) resourceLimitDecision {
	if l.exempt(clientPrefix) {
		return resourceLimitDecision{Allowed: true}
	}
	scopes := publicResourceScopes(clientPrefix)
	l.mu.Lock()
	defer l.mu.Unlock()
	retryAfter := time.Duration(0)
	refilled := make([]resourceBucket, len(scopes))
	for i, scope := range scopes {
		bucket := l.refillLocked(scope, now)
		refilled[i] = bucket
		if bucket.tokens >= 1 {
			continue
		}
		rule := l.rule(scope.Kind)
		need := time.Duration(math.Ceil((1-bucket.tokens)*rule.refill.Seconds()/float64(rule.capacity))) * time.Second
		if need <= 0 {
			need = time.Second
		}
		if retryAfter == 0 || need > retryAfter {
			retryAfter = need
		}
	}
	if retryAfter > 0 {
		for i, scope := range scopes {
			l.buckets[scope] = refilled[i]
		}
		return resourceLimitDecision{RetryAfter: retryAfter}
	}
	for i, scope := range scopes {
		bucket := refilled[i]
		bucket.tokens--
		l.buckets[scope] = bucket
	}
	return resourceLimitDecision{Allowed: true}
}

func (l *publicResourceLimiter) refillLocked(scope resourceScope, now time.Time) resourceBucket {
	rule := l.rule(scope.Kind)
	bucket, ok := l.buckets[scope]
	if !ok || bucket.updated.IsZero() {
		return resourceBucket{tokens: float64(rule.capacity), updated: now}
	}
	if now.After(bucket.updated) && rule.refill > 0 {
		bucket.tokens += float64(rule.capacity) * now.Sub(bucket.updated).Seconds() / rule.refill.Seconds()
		if bucket.tokens > float64(rule.capacity) {
			bucket.tokens = float64(rule.capacity)
		}
	}
	bucket.updated = now
	return bucket
}

func (l *publicResourceLimiter) rule(kind string) bucketRule {
	if rule, ok := l.rules[kind]; ok && rule.capacity > 0 && rule.refill > 0 {
		return rule
	}
	return bucketRule{capacity: 3600, refill: time.Hour}
}

func (l *publicResourceLimiter) resetExact(clientPrefix string) {
	if l == nil {
		return
	}
	scope, ok := exactPublicResourceScope(clientPrefix)
	if !ok {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, scope)
}

func exactPublicResourceScope(clientPrefix string) (resourceScope, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(clientPrefix))
	if err != nil {
		return resourceScope{}, false
	}
	addr := prefix.Addr()
	bits := prefix.Bits()
	if addr.Is4() {
		switch bits {
		case 32:
			return resourceScope{Kind: "ipv4_32", Key: prefix.Masked().String()}, true
		case 24:
			return resourceScope{Kind: "ipv4_24", Key: prefix.Masked().String()}, true
		}
		return resourceScope{}, false
	}
	if bits == 128 {
		return resourceScope{Kind: "ipv6_128", Key: prefix.Masked().String()}, true
	}
	return resourceScope{}, false
}

func (s Server) ResetResourceLimiter(clientPrefix string) {
	if s.ResourceLimiter != nil {
		s.ResourceLimiter.resetExact(clientPrefix)
	}
}

func (l *publicResourceLimiter) exempt(prefix string) bool {
	if prefix == "" || prefix == "unknown" || len(l.exemptions) == 0 {
		return false
	}
	scope, err := netip.ParsePrefix(strings.TrimSpace(prefix))
	if err != nil {
		return false
	}
	addr := scope.Addr()
	for _, exemption := range l.exemptions {
		if exemption.Contains(addr) {
			return true
		}
	}
	return false
}

func publicResourceScopes(prefix string) [2]resourceScope {
	host := strings.TrimSuffix(strings.TrimSuffix(prefix, "/32"), "/128")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return [2]resourceScope{
			{Kind: "unknown_address", Key: "unknown"},
			{Kind: "unknown_network", Key: "unknown"},
		}
	}
	if addr.Is4() {
		return [2]resourceScope{
			{Kind: "ipv4_32", Key: addr.String() + "/32"},
			{Kind: "ipv4_24", Key: netip.PrefixFrom(addr, 24).Masked().String()},
		}
	}
	return [2]resourceScope{
		{Kind: "ipv6_128", Key: addr.String() + "/128"},
		{Kind: "ipv6_64", Key: netip.PrefixFrom(addr, 64).Masked().String()},
	}
}
