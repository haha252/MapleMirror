package syncer

import (
	"strings"
	"testing"
)

func TestSyncLimiterIsSharedAcrossReadersAndTasks(t *testing.T) {
	limiter := NewBandwidthLimiter(1 << 30)
	first := Executor{SyncLimiter: limiter}
	second := Executor{SyncLimiter: limiter}
	r1, ok := first.rateLimitedBody(strings.NewReader("first")).(*rateLimitedReader)
	if !ok {
		t.Fatal("first reader is not rate limited")
	}
	r2, ok := second.rateLimitedBody(strings.NewReader("second")).(*rateLimitedReader)
	if !ok {
		t.Fatal("second reader is not rate limited")
	}
	if r1.shared != limiter || r2.shared != limiter || r1.shared != r2.shared {
		t.Fatal("concurrent sync tasks do not share the node-global limiter")
	}
}
