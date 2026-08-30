package developerapi

import (
	"fmt"
	"testing"
	"time"
)

func TestAuthFailureLimiterBlocksAndBoundsMemory(t *testing.T) {
	limiter := newAuthFailureLimiter()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < authFailureLimit; i++ {
		limiter.failure("192.0.2.1", now)
	}
	if blocked, _ := limiter.blocked("192.0.2.1", now); !blocked {
		t.Fatal("repeated authentication failures should block the client")
	}
	for i := 0; i < authFailureCapacity+100; i++ {
		limiter.failure(fmt.Sprintf("198.51.100.%d", i), now)
	}
	if len(limiter.items) > authFailureCapacity {
		t.Fatalf("auth failure limiter grew beyond capacity: %d", len(limiter.items))
	}
}
