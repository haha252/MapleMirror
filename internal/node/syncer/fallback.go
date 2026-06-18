package syncer

import (
	"context"
	"log/slog"
	"os"

	"mirror-server/internal/protocol"
)

func (e Executor) fetchFallback(ctx context.Context, task protocol.SyncTask, tmpPath string) (string, int64, bool, error) {
	var lastDigest string
	var lastSize int64
	var lastErr error
	attempted := false
	for _, source := range task.FallbackSources {
		_ = os.Remove(tmpPath)
		if e.Logger != nil {
			e.Logger.Warn(context.Background(), "节点尝试从其他节点复制资产",
				slog.String("task_id", task.TaskID),
				slog.String("asset_id", task.Asset.AssetID),
				slog.String("source_node_id", source.NodeID),
				slog.String("source_node_name", source.NodeName))
		}
		release, err := e.acquirePeerFallback(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if err := validateSourceURL(source.DownloadURL, e.AllowPrivateSourceURLs); err != nil {
			release()
			lastErr = err
			continue
		}
		attempted = true
		digest, size, err := e.fetchPeerSource(ctx, task, source, tmpPath)
		release()
		if err != nil {
			lastErr = err
			lastDigest = digest
			lastSize = size
			continue
		}
		return digest, size, attempted, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return lastDigest, lastSize, attempted, lastErr
}

func (e Executor) fetchPeerSource(ctx context.Context, task protocol.SyncTask,
	source protocol.SyncFallbackSource, tmpPath string) (string, int64, error) {
	if len(source.Parts) == 0 || task.Asset.SizeBytes < e.effectivePeerFallbackMinSize() {
		return e.fetchWithToken(ctx, source.DownloadURL, tmpPath, source.Token, task.Asset.SizeBytes)
	}
	return e.fetchPeerParts(ctx, task, source, tmpPath)
}

func (e Executor) effectivePeerFallbackMinSize() int64 {
	if e.PeerFallbackMinSize > 0 {
		return e.PeerFallbackMinSize
	}
	return 32 * 1024 * 1024
}
