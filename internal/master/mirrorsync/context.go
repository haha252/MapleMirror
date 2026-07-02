package mirrorsync

import (
	"context"
	"time"
)

const scanStatePersistTimeout = 10 * time.Second

func scanStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), scanStatePersistTimeout)
}
