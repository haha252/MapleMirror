package node

import (
	"context"
	"database/sql"
)

func V2ToV3(ctx context.Context, tx *sql.Tx) error {
	return ensureV3PeerFallbackResults(ctx, tx)
}
