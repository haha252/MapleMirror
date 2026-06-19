package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestPublicResourceLimiterLimitsAddressAndAPIResponse(t *testing.T) {
	srv := Server{ResourceLimiter: newPublicResourceLimiter(testResourceQuota(1, 100))}
	first := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	first.RemoteAddr = "198.51.100.9:1234"
	firstRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstRec, first)
	if firstRec.Code == http.StatusTooManyRequests {
		t.Fatalf("first request should pass limiter, status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}

	second := httptest.NewRequest(http.MethodGet, "/api/public/v1/projects", nil)
	second.RemoteAddr = "198.51.100.9:1234"
	secondRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests ||
		!strings.Contains(secondRec.Body.String(), `"PUBLIC_RESOURCE_RATE_LIMITED"`) ||
		secondRec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected API 429, status=%d headers=%v body=%s",
			secondRec.Code, secondRec.Header(), secondRec.Body.String())
	}
}

func TestPublicResourceLimiterLimitsIPv4Network(t *testing.T) {
	srv := Server{ResourceLimiter: newPublicResourceLimiter(testResourceQuota(100, 1))}
	for i, remote := range []string{"198.51.100.9:1234", "198.51.100.10:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if i == 0 && rec.Code == http.StatusTooManyRequests {
			t.Fatalf("first request should pass limiter, status=%d body=%s", rec.Code, rec.Body.String())
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second request should hit /24 limit, status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestPublicResourceLimiterExemptionsSkipLimit(t *testing.T) {
	srv := Server{ResourceLimiter: newPublicResourceLimiter(testResourceQuota(1, 1))}
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("exempt request %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
}

func TestPublicResourceLimiterUnknownClientUsesSharedBucket(t *testing.T) {
	srv := Server{ResourceLimiter: newPublicResourceLimiter(testResourceQuota(1, 100))}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		req.RemoteAddr = "not-an-ip"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if i == 0 && rec.Code == http.StatusTooManyRequests {
			t.Fatalf("first unknown request should pass limiter, status=%d body=%s", rec.Code, rec.Body.String())
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second unknown request should be limited, status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
}

func TestPublicResourceLimiterResetExactKeepsRelatedScopes(t *testing.T) {
	limiter := newPublicResourceLimiter(testResourceQuota(10, 10))
	now := time.Now().UTC()
	hostScope := resourceScope{Kind: "ipv4_32", Key: "198.51.100.9/32"}
	networkScope := resourceScope{Kind: "ipv4_24", Key: "198.51.100.0/24"}
	limiter.buckets[hostScope] = resourceBucket{tokens: 0, updated: now}
	limiter.buckets[networkScope] = resourceBucket{tokens: 0, updated: now}

	limiter.resetExact("198.51.100.9/32")

	if _, ok := limiter.buckets[hostScope]; ok {
		t.Fatal("host scope bucket should be reset")
	}
	if _, ok := limiter.buckets[networkScope]; !ok {
		t.Fatal("network scope bucket should remain")
	}
}

func testResourceQuota(address, network int) config.Quota {
	return config.Quota{
		PublicResourceBuckets: config.RequestBuckets{
			IPv432:  config.Bucket{Capacity: address, FullRefill: "1h"},
			IPv424:  config.Bucket{Capacity: network, FullRefill: "1h"},
			IPv6128: config.Bucket{Capacity: address, FullRefill: "1h"},
			IPv664:  config.Bucket{Capacity: network, FullRefill: "1h"},
		},
	}
}
