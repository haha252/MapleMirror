package syncer

import (
	"context"

	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func (e Executor) executeV2PeerBootstrap(ctx context.Context, task protocolv2.SyncTask, legacy protocol.SyncTask) (protocolv2.SyncResult, *protocolv2.SwarmManifest, bool) {
	peerExecutor := e
	peerExecutor.ForcePeerDownload = true
	var result protocol.SyncTaskResult
	for _, source := range legacy.FallbackSources {
		peerTask := legacy
		peerTask.FallbackSources = []protocol.SyncFallbackSource{source}
		// Execute owns disk reservation, bandwidth limits and verification. A
		// corrupt peer is skipped without committing bytes or reporting success.
		result = peerExecutor.Execute(ctx, peerTask)
		if result.Result == "succeeded" {
			manifest, err := e.manifestFromCommittedFile(task)
			if err != nil {
				return v2Failure(task, "temporary_error", "生成 peer 清单失败: "+err.Error()), nil, true
			}
			return v2ResultFromLegacy(task, result), manifest, true
		}
		if ctx.Err() != nil {
			break
		}
	}
	return v2ResultFromLegacy(task, result), nil, e.ForcePeerDownload || ctx.Err() != nil
}
