package syncer

import (
	"context"
	"log/slog"
	"os"

	"mirror-server/internal/protocol"
)

func (e Executor) fetchFallback(ctx context.Context, task protocol.SyncTask, tmpPath string) (string, int64, error) {
	var lastDigest string
	var lastSize int64
	var lastErr error
	for _, source := range task.FallbackSources {
		_ = os.Remove(tmpPath)
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点尝试从其他节点复制资产",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("source_node_id", source.NodeID),
				slog.String("source_node_name", source.NodeName))
		}
		if err := acquirePeerFallback(ctx); err != nil {
			lastErr = err
			continue
		}
		digest, size, err := e.fetchWithToken(ctx, source.DownloadURL, tmpPath, source.Token)
		releasePeerFallback()
		if err != nil {
			lastErr = err
			lastDigest = digest
			lastSize = size
			continue
		}
		return digest, size, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return lastDigest, lastSize, lastErr
}
