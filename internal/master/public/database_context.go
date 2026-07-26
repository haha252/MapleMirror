package public

import (
	"context"
	"time"
)

const publicDatabaseReadTimeout = 10 * time.Second

// stableDatabaseReadContext keeps short, read-only catalog lookups independent
// from an HTTP client disconnect. Interrupting a file-backed SQLite SELECT can
// retire the pooled connection while an unfinished native statement still owns
// a WAL snapshot. The hard timeout still bounds genuinely stuck queries.
func stableDatabaseReadContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), publicDatabaseReadTimeout)
}
